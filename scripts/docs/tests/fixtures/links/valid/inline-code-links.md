# Inline Code Links Test

This file contains inline code that looks like links but should be skipped.

Here is an inline code span that contains a fake link: `[text](./path/to/file.md)` and another: `![img](./img.png)`.

Here is a reference-style definition in inline code: `[ref]: ./path/to/file.md`.

But this is a real link that should still be checked: [valid target](./sub/page.md#details).

Also inline code with anchor: `[link](./other.md#section)` should be ignored.

A line with mixed: use `[text](./fake.md)` but see [real target](./sub/page.md).
