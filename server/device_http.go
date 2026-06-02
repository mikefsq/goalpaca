package alpacadev

import "context"

// deviceGet handles the common Device GET members shared by every device type.
// Returns (value, handled, err); handled is false if member is not a common one
// (the caller then tries the type-specific table).
func deviceGet(member string, d Device) (any, bool, error) {
	switch member {
	case "connected":
		return d.Connected(), true, nil
	case "connecting":
		return d.Connecting(), true, nil
	case "name":
		return d.Name(), true, nil
	case "description":
		return d.Description(), true, nil
	case "driverinfo":
		return d.DriverInfo(), true, nil
	case "driverversion":
		return d.DriverVersion(), true, nil
	case "interfaceversion":
		return d.InterfaceVersion(), true, nil
	case "supportedactions":
		return d.SupportedActions(), true, nil
	}
	// "devicestate" is handled by the router (it needs the device type to build
	// the correct per-type operational set).
	return nil, false, nil
}

// devicePut handles the common Device PUT members.
func devicePut(ctx context.Context, member string, d Device, p params) (bool, error) {
	switch member {
	case "connected":
		on, err := p.reqBool("Connected")
		if err != nil {
			return true, err
		}
		if on {
			return true, d.Connect(ctx)
		}
		return true, d.Disconnect(ctx)
	case "connect": // Platform 7 async connect
		return true, d.Connect(ctx)
	case "disconnect":
		return true, d.Disconnect(ctx)
	case "commandblind":
		cmd, _ := p.get("Command")
		raw, _ := p.reqBool("Raw")
		return true, d.CommandBlind(cmd, raw)
	}
	return false, nil
}

// devicePutValue handles the common PUT members that DO return a Value
// (Action, CommandString, CommandBool). Returns (value, handled, err).
func devicePutValue(member string, d Device, p params) (any, bool, error) {
	switch member {
	case "action":
		name, _ := p.get("Action")
		args, _ := p.get("Parameters")
		v, err := d.Action(name, args)
		return v, true, err
	case "commandstring":
		cmd, _ := p.get("Command")
		raw, _ := p.reqBool("Raw")
		v, err := d.CommandString(cmd, raw)
		return v, true, err
	case "commandbool":
		cmd, _ := p.get("Command")
		raw, _ := p.reqBool("Raw")
		v, err := d.CommandBool(cmd, raw)
		return v, true, err
	}
	return nil, false, nil
}

// stateValueArray renders a DeviceState slice as the Alpaca array of
// {Name, Value} objects.
func stateValueArray(sv []StateValue) []map[string]any {
	out := make([]map[string]any, 0, len(sv))
	for _, s := range sv {
		out = append(out, map[string]any{"Name": s.Name, "Value": s.Value})
	}
	return out
}
