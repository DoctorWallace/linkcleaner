//go:build windows

// Linkcleaner decodes URLs offline and opens a result only on an explicit click.
package main

import (
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"enlacelimpio/internal/decoder"
)

const (
	// The UI accepts at most 100,000 UTF-16 code units. The edit control has
	// one extra sentinel position so an oversized paste is detected instead
	// of silently decoding a truncated 100,000-character URL.
	maxInputChars = 100000

	wmCreate         = 0x0001
	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmSetFocus       = 0x0007
	wmGetMinMaxInfo  = 0x0024
	wmSetFont        = 0x0030
	wmCommand        = 0x0111
	wmKeyDown        = 0x0100
	wmCtlColorEdit   = 0x0133
	wmCtlColorStatic = 0x0138

	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsTabStop          = 0x00010000
	wsVScroll          = 0x00200000
	wsOverlappedWindow = 0x00CF0000
	wsExClientEdge     = 0x00000200
	wsExControlParent  = 0x00010000
	esMultiline        = 0x0004
	esAutoVScroll      = 0x0040
	esReadOnly         = 0x0800
	esWantReturn       = 0x1000
	emSetSel           = 0x00B1
	emSetLimitText     = 0x00C5
	enChange           = 0x0300
	enMaxText          = 0x0501
	colorWindow        = 5
	colorWindowText    = 8
	cfUnicodeText      = 13
	gmMoveable         = 0x0002
	vkControl          = 0x11
	vkReturn           = 0x0D
	idInput            = 101
	idPaste            = 102
	idDecode           = 103
	idClear            = 104
	idOutput           = 105
	idCopy             = 106
	idOpen             = 107
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")

	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pUpdateWindow        = user32.NewProc("UpdateWindow")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pIsDialogMessage     = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pLoadCursor          = user32.NewProc("LoadCursorW")
	pLoadIcon            = user32.NewProc("LoadIconW")
	pSendMessage         = user32.NewProc("SendMessageW")
	pSetWindowText       = user32.NewProc("SetWindowTextW")
	pGetWindowText       = user32.NewProc("GetWindowTextW")
	pGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	pSetFocus            = user32.NewProc("SetFocus")
	pGetFocus            = user32.NewProc("GetFocus")
	pGetKeyState         = user32.NewProc("GetKeyState")
	pEnableWindow        = user32.NewProc("EnableWindow")
	pMoveWindow          = user32.NewProc("MoveWindow")
	pGetClientRect       = user32.NewProc("GetClientRect")
	pAdjustWindowRectEx  = user32.NewProc("AdjustWindowRectEx")
	pGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	pGetSysColor         = user32.NewProc("GetSysColor")
	pGetSysColorBrush    = user32.NewProc("GetSysColorBrush")
	pInvalidateRect      = user32.NewProc("InvalidateRect")
	pGetDC               = user32.NewProc("GetDC")
	pReleaseDC           = user32.NewProc("ReleaseDC")
	pSetProcessDPIAware  = user32.NewProc("SetProcessDPIAware")
	pMessageBox          = user32.NewProc("MessageBoxW")
	pOpenClipboard       = user32.NewProc("OpenClipboard")
	pCloseClipboard      = user32.NewProc("CloseClipboard")
	pIsClipboardFormat   = user32.NewProc("IsClipboardFormatAvailable")
	pGetClipboardData    = user32.NewProc("GetClipboardData")
	pEmptyClipboard      = user32.NewProc("EmptyClipboard")
	pSetClipboardData    = user32.NewProc("SetClipboardData")
	pGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc         = kernel32.NewProc("GlobalAlloc")
	pGlobalLock          = kernel32.NewProc("GlobalLock")
	pGlobalUnlock        = kernel32.NewProc("GlobalUnlock")
	pGlobalFree          = kernel32.NewProc("GlobalFree")
	pGlobalSize          = kernel32.NewProc("GlobalSize")
	pCreateFont          = gdi32.NewProc("CreateFontW")
	pDeleteObject        = gdi32.NewProc("DeleteObject")
	pGetDeviceCaps       = gdi32.NewProc("GetDeviceCaps")
	pSetTextColor        = gdi32.NewProc("SetTextColor")
	pSetBkMode           = gdi32.NewProc("SetBkMode")
	pShellExecute        = shell32.NewProc("ShellExecuteW")
	pCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	pCoUninitialize      = ole32.NewProc("CoUninitialize")

	app uiState
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point
}

// Go inserts the same pointer alignment as the 64-bit Windows ABI here.
type wndClassEx struct {
	Size, Style                   uint32
	WndProc                       uintptr
	ClsExtra, WndExtra            int32
	Instance, Icon, Cursor, Brush uintptr
	MenuName, ClassName           *uint16
	IconSmall                     uintptr
}
type message struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Position       point
	Private        uint32
}
type uiState struct {
	window, instance                                      uintptr
	title, description, trustNote, inputLabel, input      uintptr
	paste, decode, clear, outputLabel, output, copy, open uintptr
	status, footer, signature                             uintptr
	font, titleFont                                       uintptr
	dpi                                                   int
	lastURL                                               string
	statusError                                           bool
	comReady                                              bool
	minWidth, minHeight                                   int32
}

func utf16ptr(text string) *uint16 {
	p, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return nil
	}
	return p
}

func scale(n int) int      { return (n*app.dpi + 48) / 96 }
func signed(n int) uintptr { return uintptr(int64(n)) }

func setText(window uintptr, text string) bool {
	p := utf16ptr(text)
	if p == nil {
		return false
	}
	r, _, _ := pSetWindowText.Call(window, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
	return r != 0
}

func send(window uintptr, msg, w, l uintptr) uintptr {
	r, _, _ := pSendMessage.Call(window, msg, w, l)
	return r
}

func setStatus(text string, isError bool) {
	app.statusError = isError
	setText(app.status, text)
	pInvalidateRect.Call(app.status, 0, 1)
}

func invalidateResult() {
	app.lastURL = ""
	setText(app.output, "")
	pEnableWindow.Call(app.copy, 0)
	pEnableWindow.Call(app.open, 0)
}

func inputChanged() {
	invalidateResult()
	n, _, _ := pGetWindowTextLength.Call(app.input)
	if n > maxInputChars {
		pEnableWindow.Call(app.decode, 0)
		setStatus("The input exceeds the 100,000-character limit. Shorten the text.", true)
		return
	}
	pEnableWindow.Call(app.decode, 1)
	setStatus("", false)
}

func readInput() (string, error) {
	n, _, _ := pGetWindowTextLength.Call(app.input)
	if n > maxInputChars {
		return "", fmt.Errorf("the input exceeds the 100,000-character limit")
	}
	buffer := make([]uint16, int(n)+1)
	r, _, _ := pGetWindowText.Call(app.input, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n > 0 && r == 0 {
		return "", fmt.Errorf("the input text could not be read")
	}
	return syscall.UTF16ToString(buffer), nil
}

func decodeInput() {
	invalidateResult()
	input, err := readInput()
	if err != nil {
		setStatus("Cannot decode: "+err.Error()+".", true)
		return
	}
	if strings.TrimSpace(input) == "" {
		setStatus("Paste a link into the input box.", true)
		pSetFocus.Call(app.input)
		return
	}
	if !showDecodeWarning() {
		setStatus("The warning could not be displayed. Please try again.", true)
		return
	}
	result, err := decoder.Decode(input)
	if err != nil {
		setStatus(err.Error(), true)
		return
	}
	if !setText(app.output, result.URL) {
		setStatus("The result could not be displayed. Please try again.", true)
		return
	}
	app.lastURL = result.URL
	pEnableWindow.Call(app.copy, 1)
	pEnableWindow.Call(app.open, 1)
	if len(result.Layers) == 0 {
		setStatus("Link preserved: no supported redirection was found.", false)
		return
	}
	setStatus("Original link recovered. Ready to copy or open.", false)
}

func showDecodeWarning() bool {
	body := utf16ptr("Only decode links from trusted sources!")
	title := utf16ptr("Linkcleaner")
	r, _, _ := pMessageBox.Call(app.window, uintptr(unsafe.Pointer(body)),
		uintptr(unsafe.Pointer(title)), 0x30) // MB_OK | MB_ICONWARNING
	runtime.KeepAlive(body)
	runtime.KeepAlive(title)
	return r == 1 // IDOK
}

func pasteAndDecode() {
	text, err := clipboardText(app.window)
	if err != nil {
		setStatus(err.Error(), true)
		return
	}
	if !setText(app.input, text) {
		setStatus("The text could not be pasted. Please try again.", true)
		return
	}
	decodeInput()
	pSetFocus.Call(app.input)
}

func copyResult() {
	if app.lastURL == "" {
		return
	}
	if err := putClipboardText(app.window, app.lastURL); err != nil {
		setStatus(err.Error(), true)
		return
	}
	setStatus("Link copied to the clipboard.", false)
}

func openResult() {
	if app.lastURL == "" {
		return
	}
	target, err := url.Parse(app.lastURL)
	if err != nil || target.Hostname() == "" || target.Opaque != "" ||
		(!strings.EqualFold(target.Scheme, "https") && !strings.EqualFold(target.Scheme, "http")) {
		setStatus("Only complete HTTP or HTTPS links can be opened.", true)
		return
	}
	if !app.comReady {
		setStatus("Windows could not prepare the browser. Copy the link and open it manually.", true)
		return
	}
	verb, link := utf16ptr("open"), utf16ptr(app.lastURL)
	if link == nil {
		setStatus("The link contains invalid text and cannot be opened.", true)
		return
	}
	// Pass the validated URL directly as lpFile. No command interpreter or
	// command-line parameters are involved, so URL characters are kept intact.
	r, _, _ := pShellExecute.Call(app.window, uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(link)), 0, 0, 1)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(link)
	if r <= 32 {
		setStatus("Windows could not open the link. Check your default browser.", true)
		return
	}
	setStatus("Link sent to your default browser.", false)
}

func clipboardText(owner uintptr) (string, error) {
	r, _, _ := pOpenClipboard.Call(owner)
	if r == 0 {
		return "", fmt.Errorf("The clipboard is unavailable. Please try again.")
	}
	defer pCloseClipboard.Call()
	r, _, _ = pIsClipboardFormat.Call(cfUnicodeText)
	if r == 0 {
		return "", fmt.Errorf("The clipboard does not contain text.")
	}
	h, _, _ := pGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", fmt.Errorf("The clipboard could not be read.")
	}
	size, _, _ := pGlobalSize.Call(h)
	if size < 2 {
		return "", fmt.Errorf("The clipboard does not contain valid text.")
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return "", fmt.Errorf("The clipboard could not be read.")
	}
	defer pGlobalUnlock.Call(h)
	count := size / 2
	if count > maxInputChars+2 {
		count = maxInputChars + 2
	}
	units := unsafe.Slice((*uint16)(unsafe.Pointer(p)), int(count))
	for i, unit := range units {
		if unit == 0 {
			if i > maxInputChars {
				return "", fmt.Errorf("The input exceeds the 100,000-character limit.")
			}
			return syscall.UTF16ToString(units[:i]), nil
		}
	}
	if count > maxInputChars {
		return "", fmt.Errorf("The input exceeds the 100,000-character limit.")
	}
	return "", fmt.Errorf("The clipboard does not contain valid text.")
}

func putClipboardText(owner uintptr, text string) error {
	units, err := syscall.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("The result cannot be copied as text.")
	}
	h, _, _ := pGlobalAlloc.Call(gmMoveable, uintptr(len(units)*2))
	if h == 0 {
		return fmt.Errorf("There is not enough memory to copy the result.")
	}
	owned := true
	defer func() {
		if owned {
			pGlobalFree.Call(h)
		}
	}()
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return fmt.Errorf("The result could not be prepared for copying.")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(units)), units)
	pGlobalUnlock.Call(h)
	r, _, _ := pOpenClipboard.Call(owner)
	if r == 0 {
		return fmt.Errorf("The clipboard is unavailable. Please try again.")
	}
	defer pCloseClipboard.Call()
	r, _, _ = pEmptyClipboard.Call()
	if r == 0 {
		return fmt.Errorf("The clipboard could not be updated.")
	}
	r, _, _ = pSetClipboardData.Call(cfUnicodeText, h)
	if r == 0 {
		return fmt.Errorf("The result could not be copied.")
	}
	owned = false // Windows now owns the memory block.
	return nil
}

func createControl(class, text string, exStyle, style uintptr, id int) uintptr {
	classP, textP := utf16ptr(class), utf16ptr(text)
	h, _, _ := pCreateWindowEx.Call(exStyle, uintptr(unsafe.Pointer(classP)),
		uintptr(unsafe.Pointer(textP)), wsChild|wsVisible|style,
		0, 0, 0, 0, app.window, uintptr(id), app.instance, 0)
	runtime.KeepAlive(classP)
	runtime.KeepAlive(textP)
	if h != 0 {
		send(h, wmSetFont, app.font, 0)
	}
	return h
}

func createControls() bool {
	app.title = createControl("STATIC", "Linkcleaner", 0, 0, 0)
	app.description = createControl("STATIC", "Recover the original URL from links wrapped by URL Defense v3.", 0, 0, 0)
	app.trustNote = createControl("STATIC", "Use only links you recognise from trusted sources.\r\nURL Defense helps protect against phishing and fraud.", 0, 0, 0)
	app.inputLabel = createControl("STATIC", "Received link", 0, 0, 0)
	app.input = createControl("EDIT", "", wsExClientEdge,
		wsTabStop|wsVScroll|esMultiline|esAutoVScroll|esWantReturn, idInput)
	app.paste = createControl("BUTTON", "Paste && decode", 0, wsTabStop, idPaste)
	app.decode = createControl("BUTTON", "Decode", 0, wsTabStop, idDecode)
	app.clear = createControl("BUTTON", "Clear", 0, wsTabStop, idClear)
	app.outputLabel = createControl("STATIC", "Recovered link", 0, 0, 0)
	app.output = createControl("EDIT", "", wsExClientEdge,
		wsTabStop|wsVScroll|esMultiline|esAutoVScroll|esReadOnly, idOutput)
	app.copy = createControl("BUTTON", "Copy link", 0, wsTabStop, idCopy)
	app.open = createControl("BUTTON", "Open in browser", 0, wsTabStop, idOpen)
	app.status = createControl("STATIC", "", 0, 0, 0)
	app.footer = createControl("STATIC", "Offline decoding · No history · No storage", 0, 0, 0)
	app.signature = createControl("STATIC", "2026 MRB-GPT", 0, 0x0002, 0) // SS_RIGHT
	for _, h := range []uintptr{app.title, app.description, app.trustNote, app.inputLabel, app.input,
		app.paste, app.decode, app.clear, app.outputLabel, app.output, app.copy, app.open,
		app.status, app.footer, app.signature} {
		if h == 0 {
			return false
		}
	}
	send(app.title, wmSetFont, app.titleFont, 0)
	send(app.input, emSetLimitText, maxInputChars+1, 0)
	pEnableWindow.Call(app.copy, 0)
	pEnableWindow.Call(app.open, 0)
	layout()
	return true
}

func move(h uintptr, x, y, w, height int) {
	pMoveWindow.Call(h, signed(scale(x)), signed(scale(y)),
		signed(scale(w)), signed(scale(height)), 1)
}

func layout() {
	if app.input == 0 {
		return
	}
	var area rect
	pGetClientRect.Call(app.window, uintptr(unsafe.Pointer(&area)))
	w, h := int(area.Right)*96/app.dpi, int(area.Bottom)*96/app.dpi
	if w < 600 {
		w = 600
	}
	if h < 550 {
		h = 550
	}
	pad, width := 24, w-48
	extra := h - 570
	inputHeight, outputHeight := 88+extra/2, 80+(extra-extra/2)
	move(app.title, pad, 18, width, 34)
	move(app.description, pad, 56, width, 24)
	move(app.trustNote, pad, 94, width, 40)
	move(app.inputLabel, pad, 150, width, 22)
	move(app.input, pad, 174, width, inputHeight)
	actions := 174 + inputHeight + 12
	move(app.paste, pad, actions, 196, 36)
	move(app.decode, pad+208, actions, 134, 36)
	move(app.clear, w-pad-96, actions, 96, 36)
	outputLabelY := actions + 54
	move(app.outputLabel, pad, outputLabelY, width, 22)
	outputY := outputLabelY + 24
	move(app.output, pad, outputY, width, outputHeight)
	copyY := outputY + outputHeight + 12
	move(app.copy, pad, copyY, 152, 36)
	move(app.open, pad+164, copyY, 176, 36)
	move(app.status, pad, copyY+44, width, 32)
	move(app.footer, pad, h-34, width-156, 22)
	move(app.signature, w-pad-140, h-34, 140, 22)
}

func windowProc(window uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmCreate:
		app.window = window
		if !createControls() {
			return ^uintptr(0)
		}
		return 0
	case wmSize:
		layout()
		return 0
	case wmSetFocus:
		if app.input != 0 {
			pSetFocus.Call(app.input)
		}
		return 0
	case wmGetMinMaxInfo:
		info := (*minMaxInfo)(unsafe.Pointer(lParam))
		info.MinTrackSize.X = app.minWidth
		info.MinTrackSize.Y = app.minHeight
		return 0
	case wmCommand:
		id, code := int(wParam&0xffff), int((wParam>>16)&0xffff)
		if id == idInput && code == enChange {
			inputChanged()
			return 0
		}
		if id == idInput && code == enMaxText {
			invalidateResult()
			setStatus("The input exceeds the 100,000-character limit. Shorten the text.", true)
			return 0
		}
		if code != 0 {
			break
		}
		switch id {
		case idPaste:
			pasteAndDecode()
		case idDecode:
			decodeInput()
		case idClear:
			setText(app.input, "")
			inputChanged()
			pSetFocus.Call(app.input)
		case idCopy:
			copyResult()
		case idOpen:
			openResult()
		default:
			break
		}
		return 0
	case wmCtlColorStatic, wmCtlColorEdit:
		color, _, _ := pGetSysColor.Call(colorWindowText)
		if lParam == app.description || lParam == app.trustNote || lParam == app.footer || lParam == app.signature {
			color = 0x006B6259 // COLORREF: muted, readable grey.
		}
		if lParam == app.status && app.statusError {
			color = 0x003030AE // COLORREF: dark red.
		}
		pSetTextColor.Call(wParam, color)
		pSetBkMode.Call(wParam, 1) // TRANSPARENT
		brush, _, _ := pGetSysColorBrush.Call(colorWindow)
		return brush
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(window, uintptr(msg), wParam, lParam)
	return r
}

func handleShortcut(msg *message) bool {
	if msg.Message != wmKeyDown {
		return false
	}
	ctrl, _, _ := pGetKeyState.Call(vkControl)
	if ctrl&0x8000 == 0 {
		return false
	}
	switch msg.WParam {
	case 'A':
		focus, _, _ := pGetFocus.Call()
		if focus == app.input || focus == app.output {
			send(focus, emSetSel, 0, ^uintptr(0))
			return true
		}
	case vkReturn:
		decodeInput()
		return true
	}
	return false
}

func createFont(pointSize, weight int) uintptr {
	face := utf16ptr("Segoe UI")
	height := -(pointSize*app.dpi + 36) / 72
	h, _, _ := pCreateFont.Call(signed(height), 0, 0, 0, uintptr(weight),
		0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	runtime.KeepAlive(face)
	return h
}

func startupError(text string) {
	body, title := utf16ptr(text), utf16ptr("Linkcleaner")
	pMessageBox.Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), 0x10)
	runtime.KeepAlive(body)
	runtime.KeepAlive(title)
}

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// ShellExecute may use COM-based protocol handlers. Keep this UI thread
	// in an STA for its lifetime, without requiring COM for offline decoding.
	comResult, _, _ := pCoInitializeEx.Call(0, 0x0002|0x0004)
	if int32(comResult) >= 0 {
		app.comReady = true
		defer pCoUninitialize.Call()
	}
	// The embedded manifest declares system DPI awareness. This call also
	// covers builds made without the manifest; it is harmless when already set.
	if pSetProcessDPIAware.Find() == nil {
		pSetProcessDPIAware.Call()
	}
	app.dpi = 96
	dc, _, _ := pGetDC.Call(0)
	if dc != 0 {
		dpi, _, _ := pGetDeviceCaps.Call(dc, 88) // LOGPIXELSX
		if dpi >= 96 && dpi <= 768 {
			app.dpi = int(dpi)
		}
		pReleaseDC.Call(0, dc)
	}
	app.instance, _, _ = pGetModuleHandle.Call(0)
	app.font, app.titleFont = createFont(11, 400), createFont(19, 600)
	if app.font == 0 || app.titleFont == 0 {
		startupError("The Linkcleaner interface could not be created.")
		return
	}
	defer pDeleteObject.Call(app.font)
	defer pDeleteObject.Call(app.titleFont)
	className := utf16ptr("LinkcleanerWindow")
	icon, _, _ := pLoadIcon.Call(app.instance, 1)
	if icon == 0 {
		icon, _, _ = pLoadIcon.Call(0, 32512)
	}
	cursor, _, _ := pLoadCursor.Call(0, 32512)
	brush, _, _ := pGetSysColorBrush.Call(colorWindow)
	class := wndClassEx{
		Size:     uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:  syscall.NewCallback(windowProc),
		Instance: app.instance, Icon: icon, Cursor: cursor,
		Brush: brush, ClassName: className, IconSmall: icon,
	}
	r, _, _ := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	if r == 0 {
		startupError("The Linkcleaner window could not be started.")
		return
	}
	minimum := rect{Right: int32(scale(680)), Bottom: int32(scale(550))}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&minimum)), wsOverlappedWindow, 0, wsExControlParent)
	app.minWidth, app.minHeight = minimum.Right-minimum.Left, minimum.Bottom-minimum.Top
	initial := rect{Right: int32(scale(760)), Bottom: int32(scale(570))}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&initial)), wsOverlappedWindow, 0, wsExControlParent)
	width, height := int(initial.Right-initial.Left), int(initial.Bottom-initial.Top)
	screenWidth, _, _ := pGetSystemMetrics.Call(0)
	screenHeight, _, _ := pGetSystemMetrics.Call(1)
	x, y := (int(screenWidth)-width)/2, (int(screenHeight)-height)/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	windowTitle := utf16ptr("Linkcleaner")
	window, _, _ := pCreateWindowEx.Call(wsExControlParent,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowTitle)),
		wsOverlappedWindow, signed(x), signed(y), signed(width), signed(height),
		0, 0, app.instance, 0)
	runtime.KeepAlive(className)
	runtime.KeepAlive(windowTitle)
	if window == 0 {
		startupError("The Linkcleaner window could not be created.")
		return
	}
	pShowWindow.Call(window, 1)
	pUpdateWindow.Call(window)
	pSetFocus.Call(app.input)
	var msg message
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) == -1 {
			startupError("The window could not process a Windows message.")
			return
		}
		if r == 0 {
			return
		}
		if handleShortcut(&msg) {
			continue
		}
		handled, _, _ := pIsDialogMessage.Call(window, uintptr(unsafe.Pointer(&msg)))
		if handled != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
