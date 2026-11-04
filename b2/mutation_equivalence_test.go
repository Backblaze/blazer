package b2

// Equivalent Gremlins v0.6.0 survivors from #290. Locations refer to the
// c3b00f1 baseline; these are code annotations, not assertions on private state.
//
// writer.go:516, copy.go:162, replication.go:251: +1 -> -1 changes only a map
// allocation hint. Maps grow as needed, including when the hint is negative.
// writer.go:610 and reader.go:286: <1 -> <=1 assigns 1 to an existing 1.
// writer.go:621: both mutations of -1 are overwritten by cidx+1, or the method
// returns on cancellation before using the initial value.
// writer.go:699 and reader.go:266: <0 -> <=0 assigns zero at zero.
// writer.go:731 and reader.go:269: the boundary mutation assigns an equal limit.
// writer.go:1017: > -> >= clamps an equal value to itself.
// writer.go:1022: <= -> < adds a CAS(previous, previous) at equality; neither
// version advances the high-water mark or reports additional progress.
// writer.go:1023: > -> >= computes a zero delta at equality.
// reader.go:459: <10s -> <=10s differs only at the unreachable value 10s.
// Each chunk starts at zero; wait initializes 1ms and doubles to 16384ms.
// replication.go:215: > -> >= assigns the same maximum at equality.
// replication.go:219: >= -> > still returns the cap via max+offset at equality.
// iterator.go:102 (the >1000 expression) and :108: >= assigns the same page cap.
// b2.go:774: >=0 -> >0 differs only for zero-length ranges, which Read handles
// before starting any workers, so the bounded flag is never consulted.
// compose.go:274: <0 -> <=0 also rejects zero, already rejected by !=want
// because every validated range contains at least one byte.
// progress.go:49: <=0 -> <0 lets zero wake the reporter; run ignores zero
// updates, so callback values and terminal delivery remain identical.
// sync/sync.go:351: >0 -> >=0 writes an empty slice to SHA-1, a no-op.
// backend.go:101: ++ -> -- changes an opaque uint64 equality token in either
// direction with the same wrap period; no caller observes its numeric value.
