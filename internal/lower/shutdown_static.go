package lower

import (
	"github.com/matjam/plutonium/internal/ssa"
	"github.com/matjam/plutonium/internal/types2"
)

// Static copies have a single typed obligation, even when their containing
// method record also has an unowned code word.
func (l *Lowerer) recordStaticCleanupAt(sym *ssa.Sym, off int64, T types2.Type, transferred bool) {
	if l.cleanupAnchor != nil && (l.needsDrop(T) || l.counted(T)) {
		l.cleanupStatics = append(l.cleanupStatics, staticCleanupObject{
			sym: sym, T: T, off: off, anchor: l.cleanupAnchor, metadataOnly: transferred,
		})
	}
}

func (l *Lowerer) staticCleanupSize(obj staticCleanupObject) int64 {
	if obj.region != nil {
		size, _ := l.keySize(*obj.region)
		return max(size, 1)
	}
	return max(l.sizeof(obj.T), 1)
}

func (l *Lowerer) recordStaticRegion(sym *ssa.Sym, key regionKey) {
	if l.cleanupAnchor != nil {
		l.cleanupStatics = append(l.cleanupStatics, staticCleanupObject{
			sym: sym, region: &key, anchor: l.cleanupAnchor,
		})
	}
}
