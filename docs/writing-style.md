# Writing style

These rules help people read Blazer documentation and godoc. They are inspired by
[ASD-STE100 Issue 9](https://www.asd-ste100.org/), but use our own words; they are not the standard, and copy no standard text or dictionary.

Apply these rules to new and edited documentation and godoc. Do not apply them to
`CHANGELOG.md`, dated records in `docs/verification/`, or historical design records.
Do not apply them to `SECURITY.md` or `CODE_OF_CONDUCT.md` until their owners agree.

## Rules

1. **Do:** Put one idea in each sentence. Aim for about 20 words or fewer in steps and 25 or fewer in descriptions.
   **Instead of:** Start the upload and check the result. **Write:** Start the upload. Check the result.
2. **Do:** Use active voice. Name who or what does the action.
   **Instead of:** The file is uploaded. **Write:** The client uploads the file.
3. **Do:** Write steps as commands. Give one action per step.
   **Instead of:** You set the flag and run the command. **Write:** Set the flag. Run the command.
4. **Do:** Use one word for one meaning, as shown in the term table.
   **Instead of:** Transfer the object. Upload the item. **Write:** Upload the object.
5. **Do:** Use simple verbs, such as use, make, start, stop, send and get. Avoid idioms and phrasal verbs.
   **Instead of:** Kick off the upload. **Write:** Start the upload.
6. **Do:** Keep articles: a, an and the.
   **Instead of:** Set flag for bucket. **Write:** Set the flag for the bucket.
7. **Do:** Use plain words. Avoid empty intensifiers such as simply, just, very, robust and seamless.
   **Instead of:** Simply send the request. **Write:** Send the request.
8. **Do:** Avoid unclear pronouns. Repeat the noun when “it” could mean two things.
   **Instead of:** The client sends the request when it starts. **Write:** The client sends the request when the client starts.
9. **Do:** Use “must” for a requirement, “can” for an ability and “should” for a recommendation.
   **Instead of:** TLS is required. The client can use TLS. **Write:** The client must use TLS.
10. **Do:** Put the condition before the action.
    **Instead of:** Retry the request if it times out. **Write:** If the request times out, retry the request.
11. **Do:** Give exact numbers and units. Avoid “a few”, “many” and “soon”.
    **Instead of:** Wait a few seconds. **Write:** Wait 3 seconds. Use this change only when 3 seconds is known to be correct.
12. **Do:** Keep paragraphs to about six sentences or fewer. Use a list for a sequence.
    **Instead of:** First set the flag. Next start the client. Then send the request. **Write:** 1. Set the flag. 2. Start the client. 3. Send the request.
13. **Do:** Never change identifiers, flags, error strings, code or URLs.
    **Instead of:** Rename `NewClient` for style. **Write:** Keep `NewClient` unchanged.
14. **Do:** Change wording only. Never change meaning.
    **Instead of:** Replace a requirement with a recommendation. **Write:** Keep the requirement.

## Terms

Public identifiers and B2 API field names never change.

| Concept | Terms |
| --- | --- |
| B2 stored data | Use “object” for a B2 object in the high-level API. Use “file” for a local file or where the B2 operation's contract calls it a file. |
| Application authorization | Use “application key” for that credential, “authorization token” for a token, and “SSE-C key” for encryption material. Never use a bare “key” when the meaning is unclear. |
| Storage container | Use “bucket”. |
| Large-file units | Use “part” for a B2 large-file part and “chunk” for an SDK buffer or download chunk. |
| Sending data | Use “upload” for a transfer to B2 and “write” for the Writer operation or writing local data. |
| Receiving data | Use “download” for a transfer from B2 and “read” for a reader operation or local input. |
| Retry | Use “retry” for repeating a request and “attempt” for counting each try, including the first. |

## Meaning-preservation checklist

- Keep numbers, units and limits unchanged.
- Keep the strength of “must”, “should” and “can” unchanged.
- Keep security wording and credential rules unchanged.
- Keep compatibility and “Breaking” statements unchanged.
- Check each statement against the code or B2 contract. If you cannot verify a statement, flag it. Do not guess.
- Do not hide a contradiction with a simpler sentence.

## Examples from Blazer documentation

**README**
Instead of: “Unless you specifically need the Native API, you should use the MinIO Go Client SDK with B2's S3 Compatible API.”
Write: “If you do not need the Native API, you should use the MinIO Go Client SDK with B2's S3 Compatible API.”

**Upgrade guide**
Instead of: “v0.10.0 adds the parity capabilities and includes breaking changes. Three require a source change (they cause compile errors); the rest change runtime behavior for specific call patterns — mostly turning a previously silent, risky behavior into an explicit error ("fail closed").”
Write: “v0.10.0 adds the parity capabilities and includes breaking changes. Three require a source change because they cause compile errors. The other changes affect runtime behavior for specific call patterns. Most replace a silent, risky behavior with an explicit error ("fail closed").”

**Testing guide**
Instead of: “Cross-compiling proves the code builds for a platform, not that it runs there; native Windows and macOS runs happen in CI.”
Write: “Cross-compiling proves that the code builds for a platform. It does not prove that the code runs there. CI also runs native Windows and macOS tests.”

## How to check

Count the words in each sentence to estimate sentence length.
Count sentences over 25 words and review them for separate ideas.
Count likely passive-voice sentences and check whether each one names the actor.
