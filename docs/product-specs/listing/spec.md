# Spec: Object listing iterator contract
From: [intent.md](intent.md) (Felipe Fumero). Status: ready for review.

This standing spec describes `286e90d`. Code citations refer to that revision. A named test supplies evidence only for its assertions. This document does not claim that tests ran against live B2.

## Requirements

- **R1. Selection.** `Bucket.List` returns an iterator. Without options it selects current unhidden objects. Call `Next` before `Object`; after `Next` returns false, check `Err`. Source: `b2/iterator.go:25`, `b2/iterator.go:95`, `b2/iterator.go:96`, `b2/iterator.go:97`. No test pins this.

- **R2. Paging.** The iterator continues through empty pages with a continuation cursor. If an empty page repeats the same nonempty cursor, it returns a not-advancing error. Source: `b2/iterator.go:83`, `b2/iterator.go:84`, `b2/iterator.go:134`. Tests: `TestListContinuesPastEmptyPageWithCursor`, `TestListFailsWhenAnEmptyPageDoesNotAdvance`.

- **R3. Prefix and delimiter.** `ListPrefix` restricts names by prefix. `ListDelimiter` specifies a path separator. Current and version requests carry these values across pages. Source: `b2/iterator.go:193`, `b2/iterator.go:201`, `b2/iterator.go:266`, `b2/iterator.go:273`, `b2/iterator.go:304`, `b2/iterator.go:311`. Test: `TestListSendsPrefixAndCursorOnEveryPage`.

- **R4. Versions.** `ListHidden` selects version listing. The version path retains `upload`, `copy` and `hide` entries and filters other actions. A nonempty next name continues even without a next ID. Source: `b2/iterator.go:116`, `b2/iterator.go:271`, `b2/iterator.go:283`. Test: `TestListVersionsContinuesWithNameOnlyCursor` pins name-only continuation and filtering a folder. For every action: No test pins this.

- **R5. Unfinished uploads.** `ListUnfinished` selects unfinished large files and carries the prefix and continuation to the next request. Source: `b2/iterator.go:185`, `b2/iterator.go:337`, `b2/iterator.go:342`. Tests: `TestRegression_ListUnfinishedHonoursPrefix`, `TestListUnfinishedSendsCursorOnEveryPage`.

- **R6. Page size.** Default or zero requests 100 for names and versions. Unfinished listings omit the count by default. Names and versions cap at 10,000; unfinished listings cap at 100. Negative values select 1,000 before the unfinished cap. Source: `b2/iterator.go:102`, `b2/iterator.go:103`, `b2/iterator.go:105`, `b2/iterator.go:107`, `b2/iterator.go:113`, `b2/iterator.go:221`. Tests: `TestListDefaultPageSizeOnWire`, `TestListPageSizeB2MaximumOnWire`, `TestMappingListPageSizeClamps`.

- **R7. Errors.** `Err` returns nil for the iterator's terminal `io.EOF`; other errors are returned. When `Next` observes an ended context, it stores `ctx.Err()`; callers can match `context.Canceled` or `context.DeadlineExceeded` with `errors.Is`. Other listing errors can pass through or be wrapped by `b2err`, which implements `Unwrap`. Other `errors.Is` or `errors.As` targets: Not specified in code or godoc. Source: `b2/iterator.go:72`, `b2/iterator.go:129`, `b2/iterator.go:130`, `b2/iterator.go:160`, `b2/b2.go:466`. No test pins this.

- **R8. Locking and concurrent use.** `ListLocker` holds the supplied lock during a page request. Safety of concurrent calls to one iterator: Not specified in code or godoc. Source: `b2/iterator.go:66`, `b2/iterator.go:232`. Test: `TestListLockerIsHeldDuringEachListRequest` pins the request lock. No test pins this.

- **R9. Ordering.** The current-object path appends each page in the order received. The service ordering contract is not specified in the iterator code or godoc. Source: `b2/iterator.go:317`. Test: `TestListSendsPrefixAndCursorOnEveryPage` pins its fixture order. No test pins this.
