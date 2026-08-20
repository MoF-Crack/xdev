# Third-party trees not in this repo

XDev does not vendor Songquanpeng One API. A leftover `one-api/`
directory in a working copy is gitignored and is not part of the
product (Spec 029 B5 / Spec 012 removed the AI assistant).

If you still have a local copy from an older checkout, delete it:

```
Remove-Item -Recurse -Force one-api
```
