package rt

// Arena identities are independent of an owner's address, so moving an
// arena preserves its marks without giving a second owner release authority.
var arenaIdentities uint64

func ArenaIdentity() uint64 {
	for {
		old := load64(&arenaIdentities)
		if old == ^uint64(0) {
			panic("arena: allocator identity exhausted")
		}
		if casUint64(&arenaIdentities, old, old+1) {
			return old + 1
		}
	}
}
