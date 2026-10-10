package main

type DeviceState union {
	Off
	Probing struct{ attempt int }
	Failed  int
}

type Slot union {
	Empty
	Full owned[int]
}

type Desc packed union uint8 {
	kind bits[uint8, 2]

	Seg case 0 { limit bits[uint8, 6] }
	Gate case 1 {}
}

func describe(st DeviceState) int {
	switch s := st.(type) {
	case Off:
		return 0
	case Probing:
		return s.attempt
	}
	return -1
}

func kind(d Desc) int {
	switch d.(type) {
	case Seg:
		return 1
	case Gate:
		return 2
	}
	return 0
}

func main() {
	var p DeviceState.Probing
	st := DeviceState{}
	switch st.(type) {
	case int:
	default:
	}
	_, _, _ = p, describe(st), kind(Desc(0))
}
