package sys

type Fd owner int32

func (f *Fd) Drop() {}

func Open() Fd { return Fd(3) } // allowed: Fd's own package

type Other owner int32
