# Spec: Object listing iterator contract
From: [intent.md](intent.md) (Felipe Fumero). Issue: #528. Status: ready for review.

This standing spec describes `286e90d`. Code citations refer to that revision. A named test supplies evidence only for its assertions. This document does not claim that tests ran against live B2.

## Requirements

- **R1. Selection.** `Bucket.List` returns an iterator. Without options it selects current unhidden objects. Call `Next` before `Object`; after `Next` returns false, check `Err`. Source: `b2/iterator.go:25`, `b2/iterator.go:95`, `b2/iterator.go:96`, `b2/iterator.go:97`. Test: `TestListDefaultPageSizeUsesB2Default` exercises the default path and iteration (`b2/listing_wire_test.go:37`).

- **R2. Paging.** The iterator continues through empty pages with a continuation cursor. If an empty page repeats the same nonempty cursor, it returns a not-advancing error. Source: `b2/iterator.go:83`, `b2/iterator.go:84`, `b2/iterator.go:134`. Tests: `TestListContinuesPastEmptyPageWithCursor`, `TestListFailsWhenAnEmptyPageDoesNotAdvance`.

- **R3. Prefix and delimiter.** `ListPrefix` restricts names by prefix. `ListDelimiter` specifies a path separator. Current and version requests carry these values across pages. Source: `b2/iterator.go:193`, `b2/iterator.go:201`, `b2/iterator.go:266`, `b2/iterator.go:273`, `b2/iterator.go:304`, `b2/iterator.go:311`. The current-object path exposes delimiter prefix entries as objects; the version path filters folder actions. A prefix entry need not be an actual object. Source: `b2/iterator.go:201`, `b2/iterator.go:279`, `b2/iterator.go:317`. `ListUnfinished` ignores `ListDelimiter`: the unfinished request passes the prefix but no delimiter, and its continuation retains no delimiter (`b2/iterator.go:337`, `b2/iterator.go:343`). No test pins this combination. Search: `rg -n "ListDelimiter|ListUnfinished" b2 --glob "*_test.go"`. Tests: `TestListSendsPrefixAndCursorOnEveryPage`, `TestListVersionsContinuesWithNameOnlyCursor`.

- **R4. Versions.** `ListHidden` selects version listing unless `ListUnfinished` is also set; unfinished listing takes precedence. The version path retains `upload`, `copy` and `hide` entries and filters other actions. A nonempty next name continues even without a next ID. Source: `b2/iterator.go:116`, `b2/iterator.go:271`, `b2/iterator.go:283`. Test: `TestListVersionsContinuesWithNameOnlyCursor` pins name-only continuation and filtering a folder. The upload and hide actions are exercised by `TestListSendsPrefixAndCursorOnEveryPage` and `TestListedObjectAttrsStatus` (`b2/listing_wire_test.go:185`, `b2/listing_wire_test.go:360`); `TestCopiedVersionIsAListedAndCompletedObject` checks copy (`b2/copy_test.go:185`).

- **R5. Unfinished uploads.** `ListUnfinished` selects unfinished large files and carries the prefix and continuation to the next request. It takes precedence over `ListHidden` when both options are supplied, regardless of option order (`b2/iterator.go:110`). No test pins this combination. Search: `rg -n "ListHidden|ListUnfinished" b2 --glob "*_test.go"`. Source: `b2/iterator.go:185`, `b2/iterator.go:337`, `b2/iterator.go:342`. Tests: `TestRegression_ListUnfinishedHonoursPrefix`, `TestListUnfinishedSendsCursorOnEveryPage`.

- **R6. Page size.** Default or zero requests 100 for names and versions. Unfinished listings omit the count by default. Names and versions cap at 10,000; unfinished listings cap at 100. Negative values select 1,000 before the unfinished cap. Source: `b2/iterator.go:102`, `b2/iterator.go:103`, `b2/iterator.go:105`, `b2/iterator.go:107`, `b2/iterator.go:113`, `b2/iterator.go:221`. These are SDK request limits. Billing limit for list calls: Not specified in code or godoc. The page-size godoc describes request counts and SDK limits (`b2/iterator.go:219`, `b2/iterator.go:224`). Tests: `TestListDefaultPageSizeOnWire`, `TestListPageSizeB2MaximumOnWire`, `TestMappingListPageSizeClamps`.

- **R7. Errors.** `Err` returns nil for the iterator's terminal `io.EOF`; other errors are returned. When `Next` observes an ended context, it stores `ctx.Err()`; callers can match `context.Canceled` or `context.DeadlineExceeded` with `errors.Is`. Other listing errors can pass through or be wrapped by `b2err`, which implements `Unwrap`. `IsNotExist` applies when the page error matches the bucket-not-found pattern and is wrapped as a not-found error (`b2/iterator.go:72`, `b2/b2.go:470`). Other `errors.Is` or `errors.As` targets: Not specified in code or godoc. Source: `b2/iterator.go:72`, `b2/iterator.go:129`, `b2/iterator.go:130`, `b2/iterator.go:160`, `b2/b2.go:466`. `TestListDefaultPageSizeOnWire` checks that normal exhaustion returns nil (`b2/listing_wire_test.go:118`). No test pins this: listing context errors or `IsNotExist` classification. Search: `rg -n "IsNotExist|context.Canceled|DeadlineExceeded|io.EOF|iter.Err" b2 --glob "*_test.go"`.

- **R8. Locking and concurrent use.** `ListLocker` holds the supplied lock during a page request only; it does not guard the iterator state. One iterator is not safe for concurrent use: callers must serialize every iterator method. Source: `b2/iterator.go:51`, `b2/iterator.go:65`, `b2/iterator.go:232`. `TestListLockerIsHeldDuringEachListRequest` checks the lock during page requests (`b2/client_options_wire_test.go:133`); it does not test concurrent iterator use. No test pins this: concurrent iterator access. Search: `rg -n "ListLocker|ObjectIterator|iterator.*concurr|concurr.*iterator" b2 --glob "*_test.go"`.

- **R9. Ordering.** The current-object path appends each page in the order received. The service ordering contract is not specified in the iterator code or godoc. Source: `b2/iterator.go:317`. Test: `TestListSendsPrefixAndCursorOnEveryPage` pins its fixture order (`b2/listing_wire_test.go:225`); it does not establish service ordering.

## Design

The existing iterator chooses a listing endpoint from its options, fetches pages as needed, and exposes each returned object through `Next` and `Object`. This spec records that behavior; it adds no behavior or implementation changes.

## How each constraint is met

| Constraint | Met by |
| --- | --- |
| Describe the existing code | R1–R9 cite the fixed baseline and describe current behavior. |
| Cite code and tests, and identify gaps | Each requirement names its evidence; unpinned behaviors include their searches. |
| Keep the prefix and cursor change spec separate | R3 and R5 describe the standing contract; the existing change spec remains separate. |

## Areas of concern

1. R5's combined-option precedence has no direct test. Owner: maintainers.
2. R8 requires callers to serialize iterator methods; the request locker does not provide iterator-wide synchronization. Owner: maintainers.
3. Service ordering and billing are not specified by the iterator code. Owner: maintainers.
