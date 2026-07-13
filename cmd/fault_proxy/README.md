# fault_proxy

A transparent Alpaca HTTP reverse proxy that injects faults into individual
device members at runtime, so an Alpaca client's error, alert, and recovery
paths can be exercised deterministically against an otherwise-correct sim.

The proxy forwards every request to the upstream device unchanged — including
the binary ImageBytes camera transport — until a fault is armed over the
control channel. Faults toggle live while the client runs, so you watch the
alert or error fire in real time.

## Run

One proxy instance per upstream device (a camera and a mount are separate
Alpaca servers):

```bash
go run ./cmd/fault_proxy -listen :11510 -upstream 127.0.0.1:11110   # mount
go run ./cmd/fault_proxy -listen :11511 -upstream 10.0.1.20:11114   # camera
#   -seed N     fixes the PRNG so jitter/flaky/lossy/chaos runs replay exactly
#   -advertise  answer UDP discovery so the proxy shows up in a Discover list
#   -discover   find the upstream via UDP discovery instead of -upstream
#     -discover-match camera   require the chosen server to host that device type
#     -discover-timeout 2s     discovery listen window
```

Point the client's device address at the proxy (`127.0.0.1:11510` etc.). A
manual/typed address bypasses UDP discovery, so the proxy fully owns the
session (the Discover button would otherwise find the real sim directly).

## Discovery

`-discover` and `-advertise` are independent and compose: find the real device
by discovery, then advertise the proxy alongside it.

### Finding the upstream (`-discover`)

With `-discover` the proxy locates its **upstream** by discovery instead of a
typed `-upstream` (it is itself a client of the device). It discovers once at
startup — before the `-advertise` responder starts, and skipping its own port on
loopback, so it never targets itself — and picks the first server found, or the
first hosting a `-discover-match` device type (e.g. `-discover-match camera`).
On a network with several servers, use `-discover-match` to disambiguate.

### Being discovered (`-advertise`)

With `-advertise`, the proxy also answers Alpaca UDP discovery (`:32227`) with
its own `-listen` port, so it appears in a discovery tool as a selectable
server. It **co-binds** the discovery port rather than owning it, so the real
device's own discovery keeps working and *both* show up in the list (same host
IP, different ports) — you pick the proxy's port to route a session through the
fault injector, or the real port to bypass it.

The co-bind uses `SO_REUSEPORT` on Linux/macOS and `SO_REUSEADDR` on Windows
(which, unlike its Unix namesake, genuinely shares a UDP port). On a platform
with neither, the proxy logs that advertising is unavailable and runs normally —
reach it by typed address.

Caveat: port sharing delivers a **broadcast** discovery probe to every co-bound
responder (so both answer), but a **directed unicast** probe reaches only one of
them. The usual "Discover" button broadcasts, so both appear; a tool that
unicasts a specific host may see only one.

Note that even with `-advertise`, a **typed address remains the more reliable
setup**: with both the proxy and the real device in the list it is easy to pick
the real one by mistake and quietly bypass the fault injector.

## Fault menu

Armed with `GET /_ctl/set?fault=<kind>&member=<name>[&value=<v>][&method=GET|PUT]`:

Add `&method=GET` or `&method=PUT` to a member fault to limit it to that HTTP verb, so a
read+write member (e.g. `cooleron`, `gain`, `binx`) can fail only the write while the read
stays healthy — e.g. `fault=fail&member=cooleron&method=PUT` fails the cooler *set* but leaves
the status *poll* working, so the client's cooler control stays enabled and its set-cooler
alert path is reachable. Without `method`, a fault applies to both verbs.

| kind | scope | effect |
|------|-------|--------|
| `fail`     | member | device error (`ErrorNumber` in the driver range) on that member |
| `notimpl`  | member | ASCOM "not implemented" (`ErrorNumber 0x400`) — tests capability probes |
| `emptyerr` | member | device error with a **blank** `ErrorMessage` — tests error-number synthesis |
| `value`    | member | override the returned `Value` (raw JSON: `true`, `0.05`; a bare word becomes a JSON string) |
| `http`     | member | return an HTTP status (`value=500`) instead of a normal reply |
| `hang`     | member | never respond (until the client cancels) — tests read timeouts |
| `latency`  | global | delay every request by `value=<ms>` |
| `drop`     | global | close the connection without responding — simulates a dead server |
| `swapbin`  | global | flip `BinX`/`BinY` `1`↔`2` on the `binx`/`biny` PUT — the upstream then bins differently than requested, so the returned frame size won't match the ROI |
| `swapdims` | global | transpose the returned ImageBytes frame (exchange `dim1`/`dim2` and the pixels) — simulates a driver that emits its array with the axes flipped; tests a client's axis-swap detect-and-correct path |
| `forcejson`| global | strip `Accept: application/imagebytes` on the `imagearray` GET so the upstream returns the JSON `ImageArray` transport — tests a client's JSON fallback decode |
| `truncate` | member | send only the leading `value=<percent>` of the response body |
| `inject`   | member | splice `value=<percent>` junk bytes into the middle of the body |
| `corrupthead` | member | flip the first `value=<n>` bytes of the body |
| `corrupttail` | member | flip the last `value=<n>` bytes of the body |
| `imgfield` | member | set one ImageBytes header field to an exact int32: `value=<field>:<int>` (`version` `errnum` `clienttxn` `servertxn` `datastart` `elemtype` `txtype` `rank` `dim1` `dim2` `dim3`) |
| `pixels`   | member | overwrite the pixel region (past `dataStart`) with a constant: `value=sat` (0xFF) or `value=zero` |
| `malform`  | member | replace the reply with unparseable JSON |
| `novalue`  | member | drop the `Value` key from the JSON reply |
| `partial-drop` | member | advertise the full length, deliver `value=<percent>`, then reset the connection |
| `dropack`  | member | let the upstream run the request, then reset without delivering the reply (lost ack) |
| `contenttype` | member | override the response `Content-Type` (`value=text/plain`; default `text/plain`) |
| `throttle` | member | slow-drip the body at `value=<bytes/sec>` |
| `jitter`   | global | random per-request delay in `value=<minMs>-<maxMs>` |
| `flaky`    | member | inject a device error on `value=<percent>` of reads |
| `lossy`    | global | reset the connection on `value=<percent>` of requests |
| `chaos` / `badwifi` | global | degraded-link combo: jitter + lossy + occasional partial frame drops |
| `failfirst`| member | fail the first `value=<n>` requests to the member, then succeed |
| `everynth` | member | fail every `value=<n>`th request to the member |

The raw-body faults (`truncate` `inject` `corrupt*` `imgfield` `pixels` `malform`)
rewrite the bytes, so they apply to the binary ImageBytes camera frame
(`member=imagearray`) as well as JSON. `swapbin` is a request-side mutation.
The random faults (`jitter` `flaky` `lossy` `chaos`) draw from a PRNG seeded by
`-seed` (0 = time-based) so a run can be replayed exactly.

`member` is the Alpaca member — the last path segment, e.g. `slewing`,
`pulseguide`, `guideraterightascension`, `imageready`, `cooleron`, `connected`.
Member matching is case-insensitive (as Alpaca requires), so `/ImageArray`
hits a fault armed on `imagearray`.

Semantics:

- **Member faults target the device API only** (`/api/...`). A member name like
  `description` also exists at `/management/v1/description`; faulting by member
  name never reaches those management/setup endpoints.
- **Precedence** per request: `/_ctl` is exempt from everything → global
  `jitter` + `latency` delays → global `drop`/`lossy`/`chaos` reset → member
  `hang` / `http` (pre-forward, never reach the upstream) → the request is
  forwarded (`swapbin` rewrites a `binx`/`biny` PUT body on the way) → the
  response is mutated. Raw-body faults apply to any content type; the JSON
  faults (`fail`/`notimpl`/`emptyerr`/`value`/`novalue`/`flaky`/`failfirst`/
  `everynth`) only touch `application/json` replies (and are skipped, with a
  one-time log line, if the reply is compressed or the wrong content family —
  e.g. a JSON fault armed on the binary `imagearray` transport).
- **One fault per member** (including `hang`): arming a fault on a member
  replaces the previous one and resets its `failfirst`/`everynth` counter.
- **`method=GET|PUT`** scopes a member fault to one verb; without it a member
  fault applies to both. It is applied atomically to the fault being armed, so
  it never rescopes a different fault. `method=` on a global fault is rejected.
- `chaos` supplies default jitter (50–800 ms) and loss (15%) only where none
  are armed explicitly, so an explicit `jitter=0-0` / `lossy=0` still wins; its
  20%-of-frames drop never overrides an explicit fault armed on `imagearray`.
- **Global toggles** (`drop`/`swapbin`/`chaos`/`forcejson`/`swapdims`) accept
  `value=off` to disarm just that one without a full `clear`.

Multi-device upstreams: a member fault matches that member on **every** device
behind the proxy (`member=connected` faults both `telescope/0` and `camera/0`
of a single `alpacasim`, and `failfirst`/`everynth` counters are shared across
them). Run one proxy per device, or `clear` between per-device tests.

Inspect and clear:

```bash
curl 'localhost:11510/_ctl/list'
curl 'localhost:11510/_ctl/clear'                 # everything
curl 'localhost:11510/_ctl/clear?member=slewing'  # one member
curl 'localhost:11510/_ctl/set?fault=swapbin&value=off'  # disarm one global toggle
```

## Timing

- **Connect-path and capability faults must be armed _before_ Connect** — the
  client reads capabilities once at connect (`connected`, `canpulseguide`,
  `slewing`, coordinates, guide rates, site, `ispulseguiding`, camera geometry).
- **Runtime faults arm anytime** — `pulseguide`, `slewing`, `imageready`,
  `imagearray`, cooler members, etc.
- `clear` between tests so a stale fault doesn't bleed into the next one.

## Recipes (PHD2 Alpaca backends)

| What to verify | Arm (when) | Expect |
|----------------|------------|--------|
| Invalid guide rates | `fault=value&member=guideraterightascension&value=0.05` | invalid-speeds alert; guide-rate read returns error |
| Slew-during-guide + the "stop guiding when slewing" toggle | `fault=value&member=slewing&value=true` (guiding) | toggle on → guiding stops + slew alert; toggle off → continues |
| Pulse-guide failure alert | `fault=fail&member=pulseguide` (guiding) | "PulseGuide command… has failed" suppressible alert |
| Connect failure alert | `fault=fail&member=connected` (pre-connect) | "connect failed: injected driver error" |
| IsPulseGuiding tolerance | `fault=notimpl&member=ispulseguiding` (pre-connect) | connects; logs "cannot check IsPulseGuiding"; guiding still works |
| Capability probe (site/coords/rates) | `fault=notimpl&member=sitelatitude` (pre-connect) | logs "cannot read site…"; no per-poll site requests |
| Blank device ErrorMessage | `fault=emptyerr&member=startexposure` (then capture) | capture fails with "device error 1035" (not blank) |
| Capture error → disconnect+reconnect | `fault=fail&member=imageready` (guiding) | "capture failed" alert, RECONNECT |
| Capture timeout | `fault=value&member=imageready&value=false` or `fault=hang&member=imagearray` | CAPT_FAIL_TIMEOUT after the readout deadline |
| Cooler set failure alert | `fault=fail&member=cooleron` | "error turning camera cooler on/off" alert |
| UI blocking / background-connect need | `fault=hang&member=connected` (pre-connect) or `fault=latency&value=35000` | UI stalls until connect is backgrounded |
| Server death / reconnect | `fault=drop` then `clear` | connection reset → client reconnect attempt |

### Binary / frame faults (camera)

| What to verify | Arm | Expect |
|----------------|-----|--------|
| Frame size ≠ requested ROI | `fault=swapbin` (guiding) | "returned a frame that does not match the requested size", RECONNECT |
| Transposed frame (axis swap) | `fault=swapdims` (guiding) | "array axes are flipped"; frame transposed back, guiding continues (no size-mismatch disconnect) |
| JSON ImageArray fallback | `fault=forcejson` (guiding) | one-time "using the slower JSON ImageArray fallback"; frames decode, guiding continues |
| Truncated ImageBytes payload | `fault=truncate&member=imagearray&value=90` | "ImageBytes payload truncated" |
| Corrupt ImageBytes header | `fault=corrupthead&member=imagearray&value=44` | header-validation error (bad version / rank / dims / data offset) |
| Extra/garbage bytes mid-frame | `fault=inject&member=imagearray&value=10` | no crash; corrupted guide frame |
| Corrupt frame tail | `fault=corrupttail&member=imagearray&value=64` | no crash; corrupted image tail |
| Negative data offset (P0-3) | `fault=imgfield&member=imagearray&value=datastart:-16` | "invalid ImageBytes data offset"; no out-of-range read |
| Implausible dimensions (P0-3) | `fault=imgfield&member=imagearray&value=dim1:2000000000` | dimension-validation error; no size-wrap over-read |
| Bad rank / version | `fault=imgfield&member=imagearray&value=rank:5` / `value=version:9` | unsupported-rank / unsupported-version error |
| Not ImageBytes content type | `fault=contenttype&member=imagearray&value=application/json` | "server did not return ImageBytes" |
| Saturated / zero frame | `fault=pixels&member=imagearray&value=sat` | frame decodes; star detection copes with a flat field |
| Mid-download link drop | `fault=partial-drop&member=imagearray&value=40` | transport read error mid-frame, RECONNECT |
| Slow link stall | `fault=throttle&member=imagearray&value=20000` | frame arrives slowly; read deadline tolerated or timed out |

### Network / structure / determinism faults

| What to verify | Arm | Expect |
|----------------|-----|--------|
| Malformed JSON reply | `fault=malform&member=slewing` | "malformed JSON response" |
| Missing Value key | `fault=novalue&member=connected` | "no Value in response" |
| Lost guide ack | `fault=dropack&member=pulseguide` (guiding) | sim runs the pulse; client sees a reset and treats it as failed |
| Intermittent read errors | `fault=flaky&member=slewing&value=25` | ~1-in-4 reads error; client retries / degrades gracefully |
| Unreliable link | `fault=lossy&value=10` | ~1-in-10 requests reset; reconnect churn |
| WiFi latency | `fault=jitter&value=50-800` | variable response delay; no false timeouts at the low end |
| Degraded-link soak | `fault=chaos` | combined jitter + resets + occasional partial frames over a session |
| Transient outage recovery | `fault=failfirst&member=connected&value=2` | first 2 connects fail, 3rd succeeds — exercises the reconnect window |
| Reproducible flapping | `fault=everynth&member=slewing&value=5` (`-seed N`) | every 5th read fails, identically across replays |

## Coverage notes

Reachable with the proxy: every JSON member fault (all connect / guide / slew /
cooler / capability alert and log paths), plus the raw-body faults above, which
cover the ImageBytes frame-size guard and the whole `getImageBytes` header
validation suite. `imgfield` hits each header branch **deterministically** by
name (unsupported version, bad rank, implausible dimensions, invalid data
offset), where `corrupthead` only perturbs bytes at random. `partial-drop`,
`throttle`, `jitter`, `lossy`, and `chaos` add the transport-level and
degraded-network paths (mid-download reset, slow-link stall, intermittent loss)
that byte-level corruption can't reach.

**Still not reachable**: host-side failures (memory-allocation failure on
`img.Init`) are not network-injectable at all; and the mount's no-PulseGuide
alert is currently guarded by a capability read that only happens at connect, so
it fires only once the driver's `CanPulseGuide` self-heal (re-probe after a
failed pulse) is implemented.

## Tests

`go test ./cmd/fault_proxy` exercises every fault end-to-end through the real
reverse proxy against an `httptest` upstream, armed via the control channel
exactly as curl would: the JSON error injections, `value`/`novalue`/`malform`
structure faults, `http` status, `hang` (client-timeout path), `latency` and
`jitter` delays, `drop`/`lossy` connection resets, `swapbin` PUT rewriting,
the raw-body mutations (with corrected Content-Length), `imgfield`/`pixels`
against a synthetic ImageBytes frame, `swapdims`/`forcejson`, `partial-drop`
(including the full-length case) / `dropack` broken transfers (asserting the
upstream still executed the `dropack` request), `throttle` pacing, the
`failfirst`/`everynth` patterns, `method=` verb scoping (GET/PUT and hang),
case-insensitive member matching, the management-endpoint exclusion, `chaos`
deferring to an explicit fault and honouring an explicit `lossy=0`,
content-family mismatch no-op, global `value=off` disarm, control-API argument
validation, and `clear` scoping (one member vs. everything). The store is
fixed-seeded in tests, so the random faults assert deterministically.

Discovery is covered too: the `-advertise` responder answers a probe with the
proxy's port (and ignores non-probes), and the `-discover` selection logic
(self-exclusion on loopback, first-server default, `-discover-match` by device
type) is unit-tested against synthetic discovery results.

## Scripted PHD2 testing

`phd2_fault_tests.py` (stdlib-only Python) closes the manual half of the loop by
driving PHD2 itself over its event-server RPC — newline-delimited JSON-RPC on
TCP 4400, enabled in PHD2 via **Tools → Enable Server**:

- **`--rpc` mode** — ad-hoc one-shot RPC for poking PHD2 while debugging: send a
  method, print the response, and echo any events seen while waiting.

  ```bash
  ./phd2_fault_tests.py --rpc get_app_state              # -> RESP "Guiding"
  ./phd2_fault_tests.py --rpc set_connected '[true]' 15  # connect, watch events 15s
  ./phd2_fault_tests.py --rpc guide '[{"pixels":1.5,"time":8,"timeout":60}, false]'
  ./phd2_fault_tests.py --rpc stop_capture
  ```

- **The regression suite** (default mode; 38 fast + 2 `--slow` +
  1 `--interactive`): arms a fault on the proxy, drives PHD2 through the
  affected flow (guide, reconnect, recalibrate), and asserts on the resulting
  `Alert`/`GuideStep`/`StarLost` events. Every fault in the menu is exercised:

  - **Mount**: pulse-guide failure, stuck-pulse drain, lost pulse ack
    (`dropack`), slew detection, `flaky` on `ispulseguiding` (mid-poll
    tolerance — guiding continues).
  - **Capture errors**: `fail`, `emptyerr` number synthesis, `malform`,
    `novalue`, bare `http` 500, `flaky` and `everynth` intermittency.
  - **ImageBytes on real frames**: `imgfield` datastart / dimensions / rank /
    version, `truncate`, `corrupthead`, `inject`, `partial-drop`,
    `contenttype`, `swapdims`, `swapbin` (via a reconnect cycle),
    `pixels` zero/sat (`StarLost` then refound), and the JSON `ImageArray`
    fallback (`forcejson` — a positive test).
  - **Network**: `jitter` and fixed `latency` soaks (guiding stays locked),
    `throttle` as both a slow-link positive test (2 MB/s) and a watchdog
    timeout (50 KB/s), `lossy` burst, `drop` (the N1 repro — the single-shot
    reconnect fails and the suite restores the camera over RPC), and a 30 s
    `chaos` survival soak.
  - **Connect-path**: `ispulseguiding`/`maxbinx` not-implemented tolerance,
    connect failure, `failfirst` transient-outage recovery, cooler status
    errors.
  - `--slow` adds the hung frame-download watchdog timeout and the
    invalid-guide-rates alert via a forced recalibration; `--interactive` adds
    the set-cooler alert (PHD2 has no RPC to toggle the cooler, so the runner
    prompts you to click it while it watches for the alert).

  ```bash
  ./phd2_fault_tests.py                  # fast suite (~5 min, needs guiding-capable setup)
  ./phd2_fault_tests.py --slow           # + the calibration-based test
  ./phd2_fault_tests.py --only imgfield  # subset by name
  ./phd2_fault_tests.py --list
  ```

  Prerequisites: both proxies running (defaults `:11510` mount / `:11511`
  camera, override with `--mount-ctl`/`--cam-ctl`), PHD2's profile pointed at
  the proxies by manual address, the event server enabled, and a guidable
  star field (the sim provides one). Capture faults are cleared the instant
  their alert lands — PHD2's reconnect is single-shot, so a fault held across
  the reconnect window drops the camera (the runner recovers by reconnecting,
  but the timing is deliberate).

## Roadmap

Implemented and proposed faults. `[x]` = built, covered by
`go test ./cmd/fault_proxy`, and smoke-tested live; `[ ]` = planned.

### Implemented

- [x] `fail` — device error (driver-range `ErrorNumber`) on a member
- [x] `notimpl` — ASCOM "not implemented" (`0x400`) — tests capability probes
- [x] `emptyerr` — device error with a blank `ErrorMessage` — tests error-number synthesis
- [x] `value` — override the returned `Value` (raw JSON; bare word → JSON string)
- [x] `http` — return an HTTP status instead of a normal reply
- [x] `hang` — never respond (until the client cancels) — read timeout / cancel
- [x] `latency` — fixed global pre-forward delay
- [x] `drop` — close before forwarding — dead/unreachable server
- [x] `swapbin` — flip `BinX`/`BinY` `1`↔`2` on the PUT — frame-size-mismatch guard
- [x] `swapdims` — transpose the returned ImageBytes frame (dim1↔dim2 + pixels) — axis-swap detect/correct path
- [x] `forcejson` — strip the ImageBytes `Accept` so the upstream returns JSON `ImageArray` — JSON fallback decode path
- [x] `truncate` — clean short body (corrected Content-Length) — payload-truncated parse error
- [x] `inject` — splice junk bytes mid-body — corrupt frame, no crash
- [x] `corrupthead` / `corrupttail` — flip first/last N bytes — header/tail corruption
- [x] `method=GET|PUT` — scope any member fault to one HTTP verb (fail the write, keep the read)

### Binary / frame precision

- [x] `imgfield` — set a specific ImageBytes header field (version/errNum/dataStart/
      txElemType/rank/dim1/dim2) to an exact value → **deterministically** hit each
      `getImageBytes` validation branch, esp. the P0-3 memory-safety inputs
      (`dataStart=-16` → `substr` throw; `dim1/dim2` huge → 32-bit size wrap)
- [x] `partial-drop` — advertise the real Content-Length, deliver N%, then **RST** →
      transport error (mid-download WiFi drop), distinct from `truncate`'s parse error
- [x] `contenttype` — override the response Content-Type → the ImageBytes
      "server did not return ImageBytes" guard (and JSON-parse of a binary body)
- [x] `pixels` — semantic frame corruption (all-saturated / all-zero) → star-detection
      / SNR robustness rather than a crash

### JSON structure

- [x] `malform` — return unparseable JSON → "malformed JSON response"
- [x] `novalue` — drop the `Value` key → "no Value in response"

### Realistic network (the reviewer's "non-ideal network" gap)

- [x] `jitter` — variable delay, random per request in `[min,max]` (WiFi latency)
- [x] `flaky` — random device error at `rate` (application-level intermittent failure)
- [x] `lossy` — random connection RST at `rate` (transport-level unreliable link)
- [x] `throttle` — slow-drip the body at `bytes/sec` — mid-transfer stall (vs. `hang`)
- [x] `dropack` — forward the request (so the sim executes it) then drop the response —
      "lost ack" semantics (e.g. a pulse the mount ran but PHD2 thinks failed)
- [x] `chaos` / `badwifi` — combined toggle: jitter + lossy + occasional partial-drop,
      for soak-testing a full guide session under a degraded link

### Determinism helpers

- [x] `failfirst N` — fail the first N requests to a member, then succeed
      (tests recovery after a transient outage; reconnect-window behavior)
- [x] `everynth N` — fail every Nth request (reproducible intermittent pattern)
- [x] `-seed` flag — replayable pseudo-random runs for `jitter`/`flaky`/`lossy`

Notes:
- `hang pulseguide` already covers the plain "no ack on a guide command" case.
- Random faults (`jitter`/`flaky`/`lossy`/`chaos`) are for soak testing; use the
  determinism helpers (`failfirst`/`everynth`/`-seed`) to reproduce a specific bug.
