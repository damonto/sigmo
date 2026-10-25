# Source and distribution licenses

Sigmo's original source code remains licensed under the [MIT License](LICENSE).

The unified Go binary links these libraries:

| Component | License | Source |
| --- | --- | --- |
| ts43-go | GNU GPL v3.0 | https://github.com/damonto/ts43-go |
| ims-go | GNU GPL v3.0 | https://github.com/damonto/ims-go |

When conveying a combined binary that incorporates these GPL libraries, follow
the GPLv3 requirements for the combination, including the applicable license
notices and access to its complete corresponding source and build instructions.
The MIT notices on Sigmo's original files remain applicable; the MIT license on
those files does not make the combined binary an MIT-only distribution. See the
[GNU licensing FAQ](https://www.gnu.org/licenses/gpl-faq.en.html#IfLibraryIsGPL).

Third-party components retain their original license and copyright notices.
In particular, ims-go's gVisor netstack has its own license, and the EVS reference
implementation has separate redistribution and patent considerations described
in its codec documentation. Adding a GPL license to the project does not replace
those notices or grant third-party patent rights.

Publish the exact SDK tags referenced in `go.mod` before releasing Sigmo binaries.
Include the appropriate notices and corresponding-source access with releases.
