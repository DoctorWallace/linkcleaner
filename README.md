# Linkcleaner

Linkcleaner is a small, portable Windows application that recovers the original URL from supported protected or tracking links. Paste a link, decode it, then copy the result or open it in your default browser.

The application is intended for Windows 10 and Windows 11 on 64-bit x86 PCs. Running the executable does not require installation, Python, Go or additional libraries.

## Download and use

Open the **Releases** section of this repository and download the Windows executable or release package. If you download a ZIP package, extract it first, then double-click `Linkcleaner.exe`.

Paste the protected link into **Received link** and click **Decode**. The **Paste & decode** button reads a link from the clipboard and decodes it in one action. The application displays a reminder to use links from trusted sources before decoding a non-empty input.

The result appears under **Recovered link**. Use **Copy link** to copy it, or **Open in browser** to open it in your default browser. Decoding alone does not open a website. **Clear** empties the fields. Editing the input clears the previous result and disables the copy and open buttons until the new input has been decoded.

`Ctrl+Enter` decodes the current input. `Ctrl+A` selects the text in the active input or output field. `Tab` and `Shift+Tab` move between controls.

## Supported formats

| Format | Supported host or pattern |
| --- | --- |
| Proofpoint URL Defense v1, v2 and v3 | `urldefense.com` and `urldefense.proofpoint.com` |
| Editorial Manager tracking | `track.editorialmanager.com/CL0/…` |

The v3 decoder supports both individual and grouped character substitutions. Supported wrappers can be removed in sequence, including a Proofpoint link containing an Editorial Manager tracking link. The decoder retains the destination's query parameters and access codes while removing the supported wrappers.

Input can include common clipboard wrappers, Markdown link syntax and HTML entities. HTML entities are normalised, so a literal entity such as `&amp;` is interpreted as `&`. Unknown redirect services are left in place. Only HTTP and HTTPS links are accepted. The application does not follow redirects on the network, and it does not recover missing, truncated or expired access codes.

## Privacy and link safety

Decoding takes place locally and works offline. Linkcleaner does not send pasted links to a server, save links to files or keep a link history. Pasting and copying use the Windows clipboard; Windows clipboard history or synchronisation settings may retain copied content. When you choose **Open in browser**, your browser's normal network, history and privacy settings apply.

**Only decode links from trusted sources.** Linkcleaner extracts the destination embedded in a supported link. It does not scan the destination, verify that a website is safe, authenticate the sender or check whether a link is still valid. Opening a recovered link directly bypasses the removed protection or tracking service for that visit. Your email's link protection settings are not changed by the application.

## Windows download warnings

The distributed executable is not digitally signed. Windows SmartScreen may show an unknown-publisher or reputation warning, and antivirus software may also flag an unfamiliar executable. The source is available here if you prefer to review and build it yourself. A SHA-256 checksum identifies the downloaded file; it does not certify that software is safe.

## Build from source

Go 1.23 or later is required to rebuild the application. Go is not required to run a release executable. The application uses the Go standard library and native Windows controls.

On a Windows x64 machine, open Command Prompt in the repository folder and run:

```bat
build_windows.bat
```

The script runs the decoder tests and builds `dist\Linkcleaner.exe`. Executables and release archives are excluded from Git and distributed through Releases.

To run only the decoder tests on a system with Go installed:

```sh
go test ./internal/decoder
```

The test fixtures use synthetic URLs and placeholder access codes.

The checked-in `cmd/enlacelimpio/rsrc_windows_amd64.syso` file embeds `app.manifest` for Windows. It is a build resource, not a release executable. If you change the manifest, regenerate that resource from the `cmd/enlacelimpio` folder:

```sh
go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -manifest app.manifest -o rsrc_windows_amd64.syso
```

Regenerating the resource can download the specified build tool. Normal builds use the resource already in the repository.

## Third-party notice

`GO_LICENSE.txt` contains the copyright and licence notice for the Go runtime and standard library included in the executable.

## References

[Proofpoint: How do I decode a rewritten URL?](https://help.proofpoint.com/Threat_Insight_Dashboard/Concepts/How_do_I_decode_a_rewritten_URL%3F)

[Microsoft: SmartScreen reputation](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)
