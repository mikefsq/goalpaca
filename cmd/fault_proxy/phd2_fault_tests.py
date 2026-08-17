#!/usr/bin/env python3
"""Scripted fault-injection regression suite for the PHD2 Alpaca backends.

Drives PHD2 over its event-server RPC (TCP 4400) and arms faults on the
fault_proxy control channels, asserting on the resulting Alert / GuideStep /
StarLost events. Automates the manually-verified matrix from the parity plan's
"Live fault-injection testing" section, so it can be rerun after every backend
change.

Prerequisites:
  - a guidable sim: the coupled mount+camera pair sharing one sky. Standalone:
    goalpaca-devices/bin/sim (both devices on :11110); or alpacahurd with
    config/hurd.sim.json (mount :11110, guide camera :11112). goalpaca's
    cmd/alpacasim is NOT guidable (camera not coupled to the mount).
  - fault_proxy in front of each device (upstreams may share a port):
      fault_proxy -listen :11510 -upstream 127.0.0.1:11110   # mount
      fault_proxy -listen :11511 -upstream 127.0.0.1:11110   # camera (standalone sim)
      (with hurd.sim.json the camera upstream is 127.0.0.1:11112)
  - PHD2 running, profile pointed at the proxies (manual address, not
    discovery), Tools -> Enable Server on (opens TCP 4400)

Usage:
  phd2_fault_tests.py                  # fast suite
  phd2_fault_tests.py --slow           # also run the calibration-based A1 test
  phd2_fault_tests.py --only imgfield  # run tests whose name contains a string
  phd2_fault_tests.py --list

Ad-hoc RPC (no tests; send one command, echo events while waiting):
  phd2_fault_tests.py --rpc get_app_state
  phd2_fault_tests.py --rpc set_connected '[true]' 15
  phd2_fault_tests.py --rpc guide '[{"pixels":1.5,"time":8,"timeout":60}, false]'

Capture faults are cleared the moment their alert lands: PHD2's reconnect is
single-shot (N1), so a fault left armed across the reconnect window drops the
camera for good. The runner still recovers (reconnect + re-guide) if that races.
"""

import argparse
import json
import socket
import sys
import time
import urllib.request

SETTLE = {"pixels": 2.0, "time": 5, "timeout": 60}


class Proxy:
    """fault_proxy control channel."""

    def __init__(self, base):
        self.base = base.rstrip("/")

    def _get(self, path):
        with urllib.request.urlopen(self.base + path, timeout=5) as r:
            return r.read()

    def set(self, query):
        self._get("/_ctl/set?" + query)

    def clear(self):
        self._get("/_ctl/clear")


class PHD2:
    """PHD2 event-server client: one persistent connection, RPC + event stream."""

    def __init__(self, host, port):
        self.sock = socket.create_connection((host, port), timeout=5)
        self.sock.settimeout(0.25)
        self.buf = b""
        self.events = []
        self.next_id = 1

    def close(self):
        self.sock.close()

    def _pump(self):
        """Read whatever is available; return any RPC responses, queue events."""
        responses = []
        try:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise ConnectionError("PHD2 closed the event-server connection")
            self.buf += chunk
        except socket.timeout:
            pass
        while b"\n" in self.buf:
            line, self.buf = self.buf.split(b"\n", 1)
            line = line.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
            except ValueError:
                continue
            if "id" in msg and ("result" in msg or "error" in msg):
                responses.append(msg)
            elif "Event" in msg:
                self.events.append(msg)
        return responses

    def call(self, method, params=None, timeout=20):
        """Send one RPC and wait for its response. Returns (result, error)."""
        rpc_id = self.next_id
        self.next_id += 1
        req = {"method": method, "id": rpc_id}
        if params is not None:
            req["params"] = params
        self.sock.sendall((json.dumps(req) + "\r\n").encode())
        deadline = time.time() + timeout
        while time.time() < deadline:
            for resp in self._pump():
                if resp.get("id") == rpc_id:
                    return resp.get("result"), resp.get("error")
        raise TimeoutError(f"no response to {method} within {timeout}s")

    def mark(self):
        """Cursor into the event stream; wait_event only sees newer events."""
        self._pump()
        return len(self.events)

    def wait_event(self, mark, pred, timeout):
        """Wait for an event matching pred, scanning from mark. None on timeout."""
        deadline = time.time() + timeout
        i = mark
        while True:
            while i < len(self.events):
                ev = self.events[i]
                i += 1
                if pred(ev):
                    return ev
            if time.time() >= deadline:
                return None
            self._pump()

    def count_events(self, mark, name):
        self._pump()
        return sum(1 for ev in self.events[mark:] if ev.get("Event") == name)

    def find_alert(self, mark, substr):
        self._pump()
        for ev in self.events[mark:]:
            if ev.get("Event") == "Alert" and substr in ev.get("Msg", ""):
                return ev
        return None


def alert_with(*substrs):
    return lambda ev: (ev.get("Event") == "Alert"
                       and any(s in ev.get("Msg", "") for s in substrs))


def event_named(name):
    return lambda ev: ev.get("Event") == name


class Harness:
    def __init__(self, phd2, mount, cam):
        self.phd2 = phd2
        self.mount = mount
        self.cam = cam

    def clear_all(self):
        self.mount.clear()
        self.cam.clear()

    def app_state(self):
        result, err = self.phd2.call("get_app_state")
        if err:
            raise RuntimeError(f"get_app_state: {err}")
        return result

    def connected(self):
        result, _ = self.phd2.call("get_connected")
        return bool(result)

    def ensure_guiding(self, timeout=90):
        if self.app_state() == "Guiding":
            return
        if not self.connected():
            _, err = self.phd2.call("set_connected", [True], timeout=60)
            if err:
                raise RuntimeError(f"set_connected(true): {err}")
        mark = self.phd2.mark()
        _, err = self.phd2.call("guide", [SETTLE, False], timeout=30)
        if err:
            raise RuntimeError(f"guide: {err}")
        if not self.phd2.wait_event(mark, event_named("GuideStep"), timeout):
            raise RuntimeError(f"guiding did not start within {timeout}s")

    def recover(self):
        """After a capture fault: wait for auto-resume, else reconnect + re-guide."""
        self.clear_all()
        mark = self.phd2.mark()
        if self.phd2.wait_event(mark, event_named("GuideStep"), 30):
            return
        try:
            self.phd2.call("stop_capture", timeout=10)
        except TimeoutError:
            pass
        time.sleep(1)
        self.ensure_guiding()

    def stop_and_disconnect(self):
        self.phd2.call("stop_capture", timeout=10)
        deadline = time.time() + 15
        while time.time() < deadline and self.app_state() in ("Guiding", "Looping", "Calibrating"):
            time.sleep(0.5)
        _, err = self.phd2.call("set_connected", [False], timeout=30)
        if err:
            raise RuntimeError(f"set_connected(false): {err}")


# ---- test cases ----------------------------------------------------------------
# Each returns a note string on pass and raises AssertionError on failure.
# Runtime tests assume guiding; connect-path tests manage the connection themselves.


def t_pulse_guide_fail(h):
    """failed PulseGuide raises the suppressible pulse-guide-failure alert."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.mount.set("fault=fail&member=pulseguide")
    try:
        ev = h.phd2.wait_event(mark, alert_with("PulseGuide command"), 20)
        assert ev, "no PulseGuide-failure alert within 20s"
    finally:
        h.mount.clear()
    mark = h.phd2.mark()
    assert h.phd2.wait_event(mark, event_named("GuideStep"), 20), "guiding did not resume"
    return "alert fired, guiding resumed"


def t_stuck_pulse_drain(h):
    """IsPulseGuiding pinned true -> drain ~1s, abort, pulse-failure alert."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.mount.set("fault=value&member=ispulseguiding&value=true")
    try:
        ev = h.phd2.wait_event(mark, alert_with("PulseGuide command"), 25)
        assert ev, "no alert from the stuck-pulse drain within 25s"
    finally:
        h.mount.clear()
    mark = h.phd2.mark()
    assert h.phd2.wait_event(mark, event_named("GuideStep"), 20), "guiding did not resume"
    return "drain aborted stuck pulse, guiding resumed"


def t_dropack_lost_pulse_ack(h):
    """dropack: mount executes the pulse but the ack is lost -> pulse-failure alert."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.mount.set("fault=dropack&member=pulseguide")
    try:
        ev = h.phd2.wait_event(mark, alert_with("PulseGuide command"), 20)
        assert ev, "no pulse-failure alert on lost ack within 20s"
    finally:
        h.mount.clear()
    mark = h.phd2.mark()
    assert h.phd2.wait_event(mark, event_named("GuideStep"), 20), "guiding did not resume"
    return "lost ack treated as pulse failure, guiding resumed"


def t_slew_check(h):
    """Slewing pinned true. Toggle on -> slew alert; off -> guiding continues."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.mount.set("fault=value&member=slewing&value=true")
    try:
        ev = h.phd2.wait_event(mark, alert_with("slewing"), 12)
        if ev:
            return "stop-guiding-when-slewing ON: slew detected, guiding stopped"
        steps = h.phd2.count_events(mark, "GuideStep")
        assert steps >= 3, f"toggle appears off but only {steps} GuideSteps and no alert"
        return f"stop-guiding-when-slewing OFF: check skipped, {steps} steps"
    finally:
        h.mount.clear()
        h.ensure_guiding()


def t_cooler_status_error(h):
    """CoolerOn GET failure makes GetCoolerStatus return an error."""
    result, err = h.phd2.call("get_cooler_status")
    if err and "unknown" in str(err.get("message", "")).lower():
        return "SKIP: PHD2 build lacks get_cooler_status"
    if err:
        return f"SKIP: cooler status unavailable ({err.get('message')})"
    h.cam.set("fault=fail&member=cooleron&method=GET")
    try:
        _, err = h.phd2.call("get_cooler_status")
        assert err, "get_cooler_status succeeded despite the armed GET fault"
    finally:
        h.cam.clear()
    _, err = h.phd2.call("get_cooler_status")
    assert not err, f"cooler status did not recover after clear: {err}"
    return "status errored under fault, recovered after clear"


def _star_lost_test(h, fill):
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set(f"fault=pixels&member=imagearray&value={fill}")
    try:
        ev = h.phd2.wait_event(mark, event_named("StarLost"), 20)
        assert ev, f"no StarLost event on {fill} frames within 20s"
    finally:
        h.cam.clear()
    mark = h.phd2.mark()
    assert h.phd2.wait_event(mark, event_named("GuideStep"), 30), "star not refound after clear"
    return f"StarLost on {fill} frame, star refound"


def t_star_lost_blank_frame(h):
    """pixels=zero blanks the frame -> StarLost; star refound after clear."""
    return _star_lost_test(h, "zero")


def t_star_lost_saturated_frame(h):
    """pixels=sat saturates the frame -> StarLost; star refound after clear."""
    return _star_lost_test(h, "sat")


def _capture_alert_test(h, arm, expect, timeout=25):
    """Common shape: arm a capture fault, expect an alert, clear at once, recover."""
    expects = (expect,) if isinstance(expect, str) else expect
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set(arm)
    try:
        ev = h.phd2.wait_event(mark, alert_with(*expects), timeout)
        assert ev, f"no alert containing any of {expects!r} within {timeout}s"
        msg = ev.get("Msg", "").split("\n")[0]
    finally:
        h.recover()
    return msg


def t_capture_fail_reconnect(h):
    """Capture error -> capture-failed alert -> auto-reconnect resumes guiding."""
    msg = _capture_alert_test(h, "fault=fail&member=imageready", "injected driver error")
    return msg


def t_emptyerr_number_synthesis(h):
    """Blank device ErrorMessage -> client synthesizes 'device error 1035'."""
    return _capture_alert_test(h, "fault=emptyerr&member=imageready", "1035")


def t_malformed_json(h):
    """Unparseable JSON reply -> clean capture-failed alert (parse error path)."""
    return _capture_alert_test(h, "fault=malform&member=imageready", "Alpaca capture failed")


def t_novalue_json(h):
    """JSON reply missing the Value key -> clean capture-failed alert."""
    return _capture_alert_test(h, "fault=novalue&member=imageready", "Alpaca capture failed")


def t_http_500_status(h):
    """Bare HTTP 500 (no Alpaca envelope) -> clean capture-failed alert."""
    return _capture_alert_test(
        h, "fault=http&member=imageready&value=500", "Alpaca capture failed")


def t_imgfield_datastart(h):
    """negative dataStart rejected, no crash."""
    return _capture_alert_test(
        h, "fault=imgfield&member=imagearray&value=datastart:-16", "data offset")


def t_imgfield_dimensions(h):
    """implausible dimensions rejected, no crash."""
    return _capture_alert_test(
        h, "fault=imgfield&member=imagearray&value=dim1:2000000000", "implausible")


def t_imgfield_rank(h):
    """Unsupported ImageBytes rank rejected."""
    return _capture_alert_test(h, "fault=imgfield&member=imagearray&value=rank:5", "rank")


def t_truncate_payload(h):
    """Short ImageBytes payload detected before any out-of-bounds read."""
    return _capture_alert_test(h, "fault=truncate&member=imagearray&value=50", "truncated")


def t_imgfield_version(h):
    """Unsupported ImageBytes metadata version rejected."""
    return _capture_alert_test(
        h, "fault=imgfield&member=imagearray&value=version:9", "ImageBytes")


def t_corrupthead_header(h):
    """Bit-flipped ImageBytes header rejected by validation, no crash."""
    return _capture_alert_test(
        h, "fault=corrupthead&member=imagearray&value=44", "Alpaca capture failed")


def t_partial_drop_transport(h):
    """Connection reset mid-frame download -> transport error, clean alert."""
    return _capture_alert_test(
        h, "fault=partial-drop&member=imagearray&value=40", "Alpaca capture failed")


def t_contenttype_not_imagebytes(h):
    """Wrong Content-Type on the frame -> rejected (not ImageBytes, not JSON)."""
    return _capture_alert_test(
        h, "fault=contenttype&member=imagearray&value=text/plain", "Alpaca capture failed")


def t_inject_no_crash(h):
    """Junk bytes spliced mid-frame: rejected or survived, but never a crash."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set("fault=inject&member=imagearray&value=10")
    try:
        ev = h.phd2.wait_event(mark, alert_with("Alpaca capture failed"), 20)
        outcome = ("rejected: " + ev.get("Msg", "").split("\n")[0]) if ev \
            else f"survived, {h.phd2.count_events(mark, 'GuideStep')} steps"
    finally:
        h.recover()
    h.app_state()  # RPC responsive == no crash
    return outcome


def t_swapdims_mismatch(h):
    """Transposed frame (axes flipped) caught by the frame-size guard."""
    return _capture_alert_test(h, "fault=swapdims", "does not match", timeout=30)


def t_forcejson_fallback(h):
    """JSON ImageArray transport (no ImageBytes) still guides via the fallback."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set("fault=forcejson")
    try:
        # JSON frames are far bulkier than ImageBytes, so allow a slow cadence.
        ev = h.phd2.wait_event(mark, event_named("GuideStep"), 45)
        assert ev, "no GuideStep on the JSON ImageArray transport within 45s"
        alert = h.phd2.find_alert(mark, "capture failed")
        assert not alert, f"capture-failed alert on JSON fallback: {alert.get('Msg')}"
    finally:
        h.cam.clear()
        h.recover()
    return "guided over JSON ImageArray, no errors"


def _degraded_link_soak(h, arm_mount, arm_cam, seconds, min_steps, what):
    """Common shape: arm a delay-only network fault, guiding must stay locked."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    if arm_mount:
        h.mount.set(arm_mount)
    if arm_cam:
        h.cam.set(arm_cam)
    try:
        time.sleep(seconds)
        steps = h.phd2.count_events(mark, "GuideStep")
        lost = h.phd2.count_events(mark, "StarLost")
        alert = h.phd2.find_alert(mark, "")
        assert steps >= min_steps, f"only {steps} GuideSteps in {seconds}s under {what}"
        assert not alert, f"alert under {what}: {alert.get('Msg')}"
        assert lost == 0, f"{lost} StarLost events under {what}"
    finally:
        h.clear_all()
    return f"{steps} steps in {seconds}s under {what}, no alerts"


def t_jitter_soak(h):
    """Degraded link (40-350ms jitter both devices): guiding stays locked."""
    return _degraded_link_soak(h, "fault=jitter&value=40-350", "fault=jitter&value=40-350",
                               20, 3, "jitter")


def t_latency_fixed_delay(h):
    """Fixed 300ms per-request latency on both devices: guiding stays locked."""
    return _degraded_link_soak(h, "fault=latency&value=300", "fault=latency&value=300",
                               20, 3, "latency")


def t_throttle_slow_link(h):
    """Frame download throttled to 2 MB/s (~2s per frame): guiding continues."""
    return _degraded_link_soak(h, None, "fault=throttle&member=imagearray&value=2000000",
                               35, 2, "throttle")


def t_flaky_ispulseguiding_tolerance(h):
    """flaky 50% on IsPulseGuiding: mid-poll read failures are treated as
    pulse-complete (M8 tolerance), so guiding continues with no alerts."""
    return _degraded_link_soak(h, "fault=flaky&member=ispulseguiding&value=50", None,
                               15, 3, "flaky IsPulseGuiding")


def t_flaky_capture_errors(h):
    """flaky 40% on ImageReady: intermittent device errors eventually fail a
    capture -> alert; guiding recovers once cleared."""
    return _capture_alert_test(h, "fault=flaky&member=imageready&value=40",
                               "Alpaca capture failed", timeout=90)


def t_everynth_periodic_failure(h):
    """Every 3rd ImageReady read fails -> deterministic capture failure -> alert."""
    return _capture_alert_test(h, "fault=everynth&member=imageready&value=3",
                               "Alpaca capture failed", timeout=30)


def t_throttle_stall_timeout(h):
    """Frame download at 50 KB/s stalls past the camera watchdog -> timeout alert."""
    return _capture_alert_test(h, "fault=throttle&member=imagearray&value=50000",
                               ("imeout", "Alpaca capture failed"), timeout=75)


def t_server_drop_n1(h):
    """drop: every request reset -> capture fails, the single-shot
    reconnect also fails, camera drops for good; RPC reconnect restores it."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set("fault=drop")
    try:
        ev = h.phd2.wait_event(mark, alert_with("Alpaca capture failed", "imeout"), 40)
        assert ev, "no capture alert while the server was dropped"
        time.sleep(3)  # let the doomed single-shot reconnect play out
    finally:
        h.recover()  # clears, then reconnects via RPC if the camera dropped
    return "server death alerted; RPC reconnect recovered after clear"


def t_lossy_unreliable_link(h):
    """lossy 40%: resets overwhelm libcurl's single keep-alive retry -> capture
    failure; recovery may need the RPC reconnect if the N1 window was hit."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set("fault=lossy&value=40")
    try:
        ev = h.phd2.wait_event(mark, alert_with("Alpaca capture failed", "imeout"), 90)
        assert ev, "no capture failure at 40% connection loss within 90s"
    finally:
        h.recover()
    return "loss burst failed a capture; recovered"


def t_chaos_survival(h):
    """chaos soak (jitter + 15% loss + partial frames): PHD2 survives 30s of a
    genuinely bad link -- no crash, and guiding restores after clear."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set("fault=chaos")
    try:
        time.sleep(30)
        h.app_state()  # RPC responsive == no crash
        steps = h.phd2.count_events(mark, "GuideStep")
        alerts = sum(1 for ev in h.phd2.events[mark:] if ev.get("Event") == "Alert")
    finally:
        h.recover()
    return f"survived: {steps} steps, {alerts} alerts in 30s of chaos; recovered"


def t_failfirst_transient_connect(h):
    """failfirst 1 on the connect PUT: first connect attempt fails, the retry
    succeeds -- transient-outage recovery semantics."""
    h.stop_and_disconnect()
    h.cam.set("fault=failfirst&member=connected&value=1&method=PUT")
    try:
        _, err = h.phd2.call("set_connected", [True], timeout=60)
        assert err, "first connect attempt succeeded despite failfirst=1"
        _, err = h.phd2.call("set_connected", [True], timeout=60)
        assert not err, f"second connect attempt failed: {err}"
    finally:
        h.cam.clear()
    h.ensure_guiding()
    return "first connect failed, retry succeeded"


def t_swapbin_size_guard(h):
    """swapbin: binning PUT flipped 1<->2 -> geometry mismatch caught before any
    out-of-bounds copy. Needs a reconnect: PHD2 caches binning and only re-sends
    it on the first capture after a fresh connect."""
    h.stop_and_disconnect()
    h.cam.set("fault=swapbin")
    try:
        _, err = h.phd2.call("set_connected", [True], timeout=60)
        assert not err, f"connect failed with swapbin armed: {err}"
        mark = h.phd2.mark()
        h.phd2.call("guide", [SETTLE, False], timeout=30)
        ev = h.phd2.wait_event(mark, alert_with("exceeds", "does not match"), 30)
        assert ev, "no size-guard alert on swapped binning within 30s"
        msg = ev.get("Msg", "").split("\n")[0]
    finally:
        h.cam.clear()
        h.recover()
    return msg


def t_a2_ispulseguiding_tolerance(h):
    """IsPulseGuiding not-implemented at connect -> connects, guiding works."""
    h.stop_and_disconnect()
    h.mount.set("fault=notimpl&member=ispulseguiding")
    try:
        _, err = h.phd2.call("set_connected", [True], timeout=60)
        assert not err, f"connect failed with IsPulseGuiding notimpl: {err}"
        mark = h.phd2.mark()
        h.ensure_guiding()
        deadline = time.time() + 20
        while time.time() < deadline and h.phd2.count_events(mark, "GuideStep") < 5:
            h.phd2._pump()
            time.sleep(0.3)
        steps = h.phd2.count_events(mark, "GuideStep")
        assert steps >= 5, f"only {steps} GuideSteps with the fault armed"
        alert = h.phd2.find_alert(mark, "PulseGuide")
        assert not alert, f"unexpected pulse alert: {alert.get('Msg')}"
    finally:
        h.mount.clear()
    return f"connected + {steps} clean steps without IsPulseGuiding"


def t_c7_optional_maxbinx(h):
    """MaxBinX not-implemented at connect is tolerated (bin defaults to 1)."""
    h.stop_and_disconnect()
    h.cam.set("fault=notimpl&member=maxbinx")
    try:
        _, err = h.phd2.call("set_connected", [True], timeout=60)
        assert not err, f"connect failed with MaxBinX notimpl: {err}"
        assert h.connected(), "gear not connected after set_connected"
    finally:
        h.cam.clear()
    h.ensure_guiding()
    return "connected with MaxBinX missing"


def t_connect_fail_alert(h):
    """Connect failure surfaces as an error instead of hanging or succeeding."""
    h.stop_and_disconnect()
    h.cam.set("fault=fail&member=connected&method=PUT")
    try:
        _, err = h.phd2.call("set_connected", [True], timeout=60)
        assert err, "set_connected succeeded despite the armed connect fault"
    finally:
        h.cam.clear()
    _, err = h.phd2.call("set_connected", [True], timeout=60)
    assert not err, f"reconnect after clear failed: {err}"
    h.ensure_guiding()
    return "connect failed under fault, reconnected after clear"


def t_hang_capture_timeout(h):
    """hang (slow): frame download never answers -> camera watchdog timeout alert."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set("fault=hang&member=imagearray")
    try:
        # Watchdog = exposure + camera-timeout setting (default 15s); allow margin.
        ev = h.phd2.wait_event(mark, alert_with("imeout", "Alpaca capture failed"), 90)
        assert ev, "no timeout/capture alert on a hung frame download within 90s"
        msg = ev.get("Msg", "").split("\n")[0]
    finally:
        h.recover()
    return msg


def t_frame_delivery_warning(h):
    """R14 (slow): sustained network latency on the camera drives the per-frame delivery
    overhead past the warn threshold, raising the one-time slow-frame-delivery alert. Needs
    ~20 frames of stable stats, so it is a minutes-long soak. Latency (not a failure) keeps
    guiding alive, just slow."""
    h.ensure_guiding()
    mark = h.phd2.mark()
    h.cam.set("fault=latency&value=1600")  # ~3s overhead/frame (imageready + imagearray)
    try:
        ev = h.phd2.wait_event(mark, alert_with("frame delivery"), 180)
        assert ev, "no slow-frame-delivery alert after sustained latency (>=20 frames)"
        msg = ev.get("Msg", "").split("\n")[0]
    finally:
        h.clear_all()
    mark = h.phd2.mark()
    assert h.phd2.wait_event(mark, event_named("GuideStep"), 30), "guiding did not recover after clear"
    return msg


def t_set_cooler_alert(h):
    """cooler set PUT fails while the status read stays healthy
    -> 'error turning camera cooler' alert. PHD2 has no RPC to toggle the cooler,
    so this test asks the operator to click it."""
    if not h.connected():
        _, err = h.phd2.call("set_connected", [True], timeout=60)
        assert not err, f"set_connected(true): {err}"
    _, err = h.phd2.call("get_cooler_status")
    if err:
        return f"SKIP: no cooler on this camera ({err.get('message')})"
    mark = h.phd2.mark()
    h.cam.set("fault=fail&member=cooleron&method=PUT")
    try:
        print("\n  >>> ACTION: toggle the camera cooler in PHD2's camera settings "
              "(within 90s)...", flush=True)
        ev = h.phd2.wait_event(mark, alert_with("cooler"), 90)
        assert ev, "no cooler alert -- was the cooler toggled?"
        msg = ev.get("Msg", "").split("\n")[0]
    finally:
        h.cam.clear()
    return msg


def t_a1_invalid_guide_rates(h):
    """bogus guide rate read at calibration -> invalid-speeds alert."""
    h.ensure_guiding()
    h.phd2.call("stop_capture", timeout=10)
    time.sleep(2)
    mark = h.phd2.mark()
    h.mount.set("fault=value&member=guideraterightascension&value=0.05")
    try:
        _, err = h.phd2.call("guide", [SETTLE, True], timeout=30)  # recalibrate
        assert not err, f"guide(recalibrate): {err}"
        ev = h.phd2.wait_event(mark, alert_with("invalid guide speeds"), 240)
        assert ev, "no invalid-guide-speeds alert during calibration"
    finally:
        h.mount.clear()
    mark = h.phd2.mark()
    assert h.phd2.wait_event(mark, event_named("GuideStep"), 300), \
        "guiding did not start after calibration"
    return "invalid-speeds alert during calibration, guiding resumed"


# Categories: "fast" always runs; "slow" needs --slow (long waits); "interactive"
# needs --interactive (a step only a human at the PHD2 UI can perform).
TESTS = [
    ("pulse_guide_fail", t_pulse_guide_fail, "fast"),
    ("stuck_pulse_drain", t_stuck_pulse_drain, "fast"),
    ("dropack_lost_pulse_ack", t_dropack_lost_pulse_ack, "fast"),
    ("slew_check", t_slew_check, "fast"),
    ("cooler_status_error", t_cooler_status_error, "fast"),
    ("star_lost_blank_frame", t_star_lost_blank_frame, "fast"),
    ("star_lost_saturated_frame", t_star_lost_saturated_frame, "fast"),
    ("capture_fail_reconnect", t_capture_fail_reconnect, "fast"),
    ("emptyerr_number_synthesis", t_emptyerr_number_synthesis, "fast"),
    ("malformed_json", t_malformed_json, "fast"),
    ("novalue_json", t_novalue_json, "fast"),
    ("http_500_status", t_http_500_status, "fast"),
    ("imgfield_datastart", t_imgfield_datastart, "fast"),
    ("imgfield_dimensions", t_imgfield_dimensions, "fast"),
    ("imgfield_rank", t_imgfield_rank, "fast"),
    ("imgfield_version", t_imgfield_version, "fast"),
    ("truncate_payload", t_truncate_payload, "fast"),
    ("corrupthead_header", t_corrupthead_header, "fast"),
    ("inject_no_crash", t_inject_no_crash, "fast"),
    ("partial_drop_transport", t_partial_drop_transport, "fast"),
    ("contenttype_not_imagebytes", t_contenttype_not_imagebytes, "fast"),
    ("swapdims_mismatch", t_swapdims_mismatch, "fast"),
    ("forcejson_fallback", t_forcejson_fallback, "fast"),
    ("jitter_soak", t_jitter_soak, "fast"),
    ("latency_fixed_delay", t_latency_fixed_delay, "fast"),
    ("throttle_slow_link", t_throttle_slow_link, "fast"),
    ("flaky_ispulseguiding_tolerance", t_flaky_ispulseguiding_tolerance, "fast"),
    ("flaky_capture_errors", t_flaky_capture_errors, "fast"),
    ("everynth_periodic_failure", t_everynth_periodic_failure, "fast"),
    ("throttle_stall_timeout", t_throttle_stall_timeout, "fast"),
    ("server_drop_n1", t_server_drop_n1, "fast"),
    ("lossy_unreliable_link", t_lossy_unreliable_link, "fast"),
    ("chaos_survival", t_chaos_survival, "fast"),
    ("swapbin_size_guard", t_swapbin_size_guard, "fast"),
    ("failfirst_transient_connect", t_failfirst_transient_connect, "fast"),
    ("a2_ispulseguiding_tolerance", t_a2_ispulseguiding_tolerance, "fast"),
    ("c7_optional_maxbinx", t_c7_optional_maxbinx, "fast"),
    ("connect_fail_alert", t_connect_fail_alert, "fast"),
    ("hang_capture_timeout", t_hang_capture_timeout, "slow"),
    ("a1_invalid_guide_rates", t_a1_invalid_guide_rates, "slow"),
    ("frame_delivery_warning", t_frame_delivery_warning, "slow"),
    ("set_cooler_alert", t_set_cooler_alert, "interactive"),
]


def run_rpc(args):
    """--rpc mode: send one command, print RESP, echo events until the wait expires."""
    method = args.rpc[0]
    params = json.loads(args.rpc[1]) if len(args.rpc) > 1 and args.rpc[1] else None
    wait = float(args.rpc[2]) if len(args.rpc) > 2 else 4.0
    try:
        phd2 = PHD2(args.phd2_host, args.phd2_port)
    except OSError as e:
        print(f"cannot reach PHD2 event server at {args.phd2_host}:{args.phd2_port}: {e}")
        return 2
    mark = phd2.mark()
    rc = 0
    try:
        result, err = phd2.call(method, params, timeout=max(wait, 10))
        print("RESP", json.dumps(err if err else result))
        rc = 1 if err else 0
    except TimeoutError as e:
        print("TIMEOUT", e)
        rc = 1
    deadline = time.time() + wait
    while time.time() < deadline:
        phd2._pump()
        while mark < len(phd2.events):
            ev = phd2.events[mark]
            mark += 1
            print("EVT ", json.dumps(
                {k: v for k, v in ev.items() if k not in ("Timestamp", "Host", "Inst")}))
    phd2.close()
    return rc


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--phd2-host", default="127.0.0.1")
    ap.add_argument("--phd2-port", type=int, default=4400)
    ap.add_argument("--mount-ctl", default="http://127.0.0.1:11510")
    ap.add_argument("--cam-ctl", default="http://127.0.0.1:11511")
    ap.add_argument("--only", help="run only tests whose name contains this string")
    ap.add_argument("--slow", action="store_true", help="include slow tests (long waits)")
    ap.add_argument("--interactive", action="store_true",
                    help="include tests needing a human at the PHD2 UI")
    ap.add_argument("--list", action="store_true", help="list tests and exit")
    ap.add_argument("--rpc", nargs="+", metavar=("METHOD", "PARAMS_JSON [WAIT_S]"),
                    help="no tests: send one RPC, print the response, echo events while waiting")
    args = ap.parse_args()

    if args.rpc:
        return run_rpc(args)

    include = {"fast"} | ({"slow"} if args.slow else set()) \
        | ({"interactive"} if args.interactive else set())
    selected = [(n, f, c) for n, f, c in TESTS
                if c in include and (not args.only or args.only in n)]
    if args.list:
        for name, func, cat in selected:
            tag = "" if cat == "fast" else f" [{cat}]"
            print(f"{name:32s}{tag:14s} {func.__doc__.split(chr(10))[0]}")
        return 0
    if not selected:
        print(f"no tests match --only {args.only!r}")
        return 2

    try:
        phd2 = PHD2(args.phd2_host, args.phd2_port)
    except OSError as e:
        print(f"cannot reach PHD2 event server at {args.phd2_host}:{args.phd2_port}: {e}")
        print("(is PHD2 running with Tools -> Enable Server on?)")
        return 2
    h = Harness(phd2, Proxy(args.mount_ctl), Proxy(args.cam_ctl))
    h.clear_all()  # no stale faults from a previous run

    results = []
    for name, func, _cat in selected:
        t0 = time.time()
        try:
            note = func(h)
            status = "SKIP" if note.startswith("SKIP") else "PASS"
        except AssertionError as e:
            status, note = "FAIL", str(e)
        except Exception as e:  # infrastructure trouble, not a test verdict
            status, note = "ERROR", f"{type(e).__name__}: {e}"
        finally:
            try:
                h.clear_all()
            except OSError:
                pass
        elapsed = time.time() - t0
        results.append((name, status, elapsed, note))
        print(f"{status:5s} {name:30s} {elapsed:5.1f}s  {note}")

    phd2.close()
    fails = [r for r in results if r[1] in ("FAIL", "ERROR")]
    print(f"\n{len(results) - len(fails)}/{len(results)} passed"
          + (f", {len(fails)} FAILED: {', '.join(r[0] for r in fails)}" if fails else ""))
    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
