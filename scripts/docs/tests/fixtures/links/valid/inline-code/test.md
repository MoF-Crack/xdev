# Inline Code Links Isolated Test

This file contains inline code that looks like links but should be skipped.

Inline code span with fake link: `[text](./path/to/file.md)` and image: `![img](./img.png)`.

Reference-style in code: `[ref]: ./other.md`.

Inline code with anchor: `[link](./fake.md#section)`.

Mixed on one line: use `[text](./also-fake.md)` but see [real target](./target.md#hello).

Another real link: [go to target](./target.md).

Also: `[ref]: ./should-be-skipped.md` is in backticks.
