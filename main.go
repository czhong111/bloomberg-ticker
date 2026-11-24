package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	windowHeight      = 94
	controlPanelWidth = 236
	titleBarHeight    = 36
	timerID           = 1
	timerPeriod       = 33

	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmPaint       = 0x000F
	wmEraseBkgnd  = 0x0014
	wmNcHitTest   = 0x0084
	wmTimer       = 0x0113
	wmLButtonDown = 0x0201

	wsPopup      = 0x80000000
	wsThickFrame = 0x00040000
	wsExTopmost  = 0x00000008
	wsExToolwind = 0x00000080

	smCxScreen     = 0
	smCyScreen     = 1
	spiGetWorkArea = 0x0030
	transparent    = 1

	htClient      = 1
	htCaption     = 2
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procSetWindowPos           = user32.NewProc("SetWindowPos")
	procShowWindow             = user32.NewProc("ShowWindow")
	procUpdateWindow           = user32.NewProc("UpdateWindow")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procSetTimer               = user32.NewProc("SetTimer")
	procKillTimer              = user32.NewProc("KillTimer")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procGetClientRect          = user32.NewProc("GetClientRect")
	procGetWindowRect          = user32.NewProc("GetWindowRect")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procSystemParametersInfoW  = user32.NewProc("SystemParametersInfoW")
	procLoadCursorW            = user32.NewProc("LoadCursorW")
	procBeginPaint             = user32.NewProc("BeginPaint")
	procEndPaint               = user32.NewProc("EndPaint")
	procInvalidateRect         = user32.NewProc("InvalidateRect")
	procMessageBoxW            = user32.NewProc("MessageBoxW")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procFillRect               = user32.NewProc("FillRect")
	procCreateFontW            = gdi32.NewProc("CreateFontW")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procTextOutW               = gdi32.NewProc("TextOutW")
	procGetTextExtent          = gdi32.NewProc("GetTextExtentPoint32W")
	procSaveDC                 = gdi32.NewProc("SaveDC")
	procRestoreDC              = gdi32.NewProc("RestoreDC")
	procIntersectClip          = gdi32.NewProc("IntersectClipRect")
)

type point struct {
	X int32
	Y int32
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

type message struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
	Private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	SmallIcon  uintptr
}

type rssItem struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
}

type rssDocument struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type newsItem struct {
	Source string
	Title  string
}

type feed struct {
	source string
	url    string
}

var feeds = []feed{
	{source: "美国 · NPR", url: "https://feeds.npr.org/1001/rss.xml"},
	{source: "欧洲 · The Guardian", url: "https://www.theguardian.com/world/rss"},
	{source: "欧洲 · France 24", url: "https://www.france24.com/en/rss"},
	{source: "香港 · RTHK", url: "https://rthk.hk/rthk/news/rss/e_expressnews_clocal.xml"},
	{source: "香港 · SCMP", url: "https://www.scmp.com/rss/91/feed"},
	{source: "新加坡 · CNA", url: "https://www.channelnewsasia.com/rssfeeds/8395886"},
	{source: "日本 · NHK WORLD", url: "https://www3.nhk.or.jp/nhkworld/en/news/list/rss.xml"},
	{source: "日本 · Japan Times", url: "https://www.japantimes.co.jp/feed/"},
}

var (
	dataMu        sync.RWMutex
	newsItems     []newsItem
	statusText    = "正在连接实时新闻源…"
	tickerText    = "正在连接实时新闻源…"
	tickerVersion uint64

	scrollOffset          float64
	tickerSpan            int32
	tickerMeasuredVersion uint64
	paused                bool
	windowHandle          uintptr
	brandFont             uintptr
	tickerFont            uintptr
	smallFont             uintptr
	background            uintptr
	headerBrush           uintptr
)

func main() {
	if err := run(); err != nil {
		showError(err)
		os.Exit(1)
	}
}

func run() error {
	// Keep the Win32 window and its message pump on the thread that created them.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	instance, _, _ := procGetModuleHandleW.Call(0)
	if instance == 0 {
		return errors.New("无法获取程序实例")
	}

	className, _ := syscall.UTF16PtrFromString("NorthstarNewsTicker")
	cursor, _, _ := procLoadCursorW.Call(0, 32512)
	class := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(windowProc),
		Instance:  instance,
		Cursor:    cursor,
		ClassName: className,
	}
	if atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("注册窗口失败: %v", callErr)
	}

	brandFont = createFont(-19, 700)
	tickerFont = createFont(-21, 400)
	smallFont = createFont(-14, 400)
	background, _, _ = procCreateSolidBrush.Call(uintptr(rgb(10, 15, 23)))
	headerBrush, _, _ = procCreateSolidBrush.Call(uintptr(rgb(18, 27, 38)))
	if brandFont == 0 || tickerFont == 0 || smallFont == 0 || background == 0 || headerBrush == 0 {
		cleanupGDI()
		return errors.New("初始化窗口绘图资源失败")
	}

	workArea, err := getWorkArea()
	if err != nil {
		cleanupGDI()
		return err
	}
	screenWidth := workArea.Right - workArea.Left
	title, _ := syscall.UTF16PtrFromString("Northstar | 实时新闻")
	windowHandle, _, _ = procCreateWindowExW.Call(
		wsExTopmost|wsExToolwind,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup|wsThickFrame|0x10000000,
		uintptr(workArea.Left), uintptr(workArea.Bottom-windowHeight), uintptr(screenWidth), windowHeight,
		0, 0, instance, 0,
	)
	if windowHandle == 0 {
		cleanupGDI()
		return errors.New("创建新闻条窗口失败")
	}

	if timer, _, callErr := procSetTimer.Call(windowHandle, timerID, timerPeriod, 0); timer == 0 {
		procDestroyWindow.Call(windowHandle)
		cleanupGDI()
		return fmt.Errorf("启动滚动动画失败: %v", callErr)
	}
	procShowWindow.Call(windowHandle, 5)
	procUpdateWindow.Call(windowHandle)

	go refreshNewsLoop()

	var msg message
	for {
		result, _, callErr := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			procKillTimer.Call(windowHandle, timerID)
			cleanupGDI()
			return fmt.Errorf("读取窗口消息失败: %v", callErr)
		}
		if result == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	cleanupGDI()
	return nil
}

func createFont(height, weight int32) uintptr {
	name, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	font, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0, uintptr(weight),
		0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(name)),
	)
	return font
}

func cleanupGDI() {
	for _, handle := range []uintptr{brandFont, tickerFont, smallFont, background, headerBrush} {
		if handle != 0 {
			procDeleteObject.Call(handle)
		}
	}
}

func getWorkArea() (rect, error) {
	var area rect
	result, _, callErr := procSystemParametersInfoW.Call(
		spiGetWorkArea,
		0,
		uintptr(unsafe.Pointer(&area)),
		0,
	)
	if result == 0 {
		return rect{}, fmt.Errorf("读取屏幕工作区失败: %v", callErr)
	}
	return area, nil
}

func dockWindow(hwnd uintptr) error {
	var bounds rect
	if result, _, callErr := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds))); result == 0 {
		return fmt.Errorf("读取新闻条位置失败: %v", callErr)
	}
	area, err := getWorkArea()
	if err != nil {
		return err
	}
	width := bounds.Right - bounds.Left
	height := bounds.Bottom - bounds.Top
	if width > area.Right-area.Left {
		width = area.Right - area.Left
	}
	result, _, callErr := procSetWindowPos.Call(
		hwnd,
		^uintptr(0),
		uintptr(area.Left),
		uintptr(area.Bottom-height),
		uintptr(width),
		uintptr(height),
		0x0040,
	)
	if result == 0 {
		return fmt.Errorf("贴靠任务栏失败: %v", callErr)
	}
	return nil
}

func windowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmNcHitTest:
		return hitTestWindow(hwnd, lParam)
	case wmSize:
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmPaint:
		paintWindow(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmTimer:
		if wParam == timerID {
			if !paused {
				scrollOffset += 2.2
				var bounds rect
				procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
				tickerArea := rect{
					Left:   256,
					Top:    titleBarHeight,
					Right:  bounds.Right - controlPanelWidth,
					Bottom: bounds.Bottom,
				}
				procInvalidateRect.Call(hwnd, uintptr(unsafe.Pointer(&tickerArea)), 0)
			}
		}
		return 0
	case wmLButtonDown:
		var bounds rect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
		x := int32(int16(lParam & 0xffff))
		y := int32(int16((lParam >> 16) & 0xffff))
		if y < titleBarHeight {
			switch {
			case x >= bounds.Right-controlPanelWidth && x < bounds.Right-160:
				paused = !paused
				controls := rect{
					Left:   bounds.Right - controlPanelWidth,
					Top:    0,
					Right:  bounds.Right,
					Bottom: titleBarHeight,
				}
				procInvalidateRect.Call(hwnd, uintptr(unsafe.Pointer(&controls)), 0)
				return 0
			case x >= bounds.Right-160 && x < bounds.Right-82:
				if err := dockWindow(hwnd); err != nil {
					showError(err)
				}
				return 0
			case x >= bounds.Right-82:
				procDestroyWindow.Call(hwnd)
				return 0
			}
		}
	case wmDestroy:
		procKillTimer.Call(hwnd, timerID)
		procPostQuitMessage.Call(0)
		return 0
	}

	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return result
}

func hitTestWindow(hwnd, lParam uintptr) uintptr {
	var bounds rect
	if result, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds))); result == 0 {
		return htClient
	}
	x := int32(int16(lParam & 0xffff))
	y := int32(int16((lParam >> 16) & 0xffff))
	const border = 8

	left := x < bounds.Left+border
	right := x >= bounds.Right-border
	top := y < bounds.Top+border
	bottom := y >= bounds.Bottom-border
	switch {
	case left && top:
		return htTopLeft
	case right && top:
		return htTopRight
	case left && bottom:
		return htBottomLeft
	case right && bottom:
		return htBottomRight
	case left:
		return htLeft
	case right:
		return htRight
	case top:
		return htTop
	case bottom:
		return htBottom
	case y < bounds.Top+titleBarHeight && x >= bounds.Right-controlPanelWidth:
		return htClient
	case y < bounds.Top+titleBarHeight:
		return htCaption
	default:
		return htClient
	}
}

func paintWindow(hwnd uintptr) {
	var ps paintStruct
	targetDC, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var bounds rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
	memoryDC, _, _ := procCreateCompatibleDC.Call(targetDC)
	if memoryDC == 0 {
		drawWindow(targetDC, bounds)
		return
	}
	bitmap, _, _ := procCreateCompatibleBitmap.Call(targetDC, uintptr(bounds.Right), uintptr(bounds.Bottom))
	if bitmap == 0 {
		procDeleteDC.Call(memoryDC)
		drawWindow(targetDC, bounds)
		return
	}
	oldBitmap, _, _ := procSelectObject.Call(memoryDC, bitmap)
	drawWindow(memoryDC, bounds)
	if result, _, _ := procBitBlt.Call(
		targetDC, 0, 0, uintptr(bounds.Right), uintptr(bounds.Bottom),
		memoryDC, 0, 0, 0x00CC0020,
	); result == 0 {
		drawWindow(targetDC, bounds)
	}
	procSelectObject.Call(memoryDC, oldBitmap)
	procDeleteObject.Call(bitmap)
	procDeleteDC.Call(memoryDC)
}

func drawWindow(hdc uintptr, bounds rect) {
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&bounds)), background)
	header := rect{Left: 0, Top: 0, Right: 256, Bottom: bounds.Bottom}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&header)), headerBrush)
	drawText(hdc, 20, 8, "NORTHSTAR", rgb(92, 207, 220), brandFont)
	drawText(hdc, 20, 43, "GLOBAL NEWS  /  国际快讯", rgb(143, 160, 177), smallFont)
	drawText(hdc, 184, 13, "LIVE", rgb(255, 92, 92), smallFont)

	separator := rect{Left: 256, Top: 17, Right: 257, Bottom: 59}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&separator)), headerBrush)

	dataMu.RLock()
	messageText := tickerText
	currentStatus := statusText
	version := tickerVersion
	dataMu.RUnlock()

	statusColor := rgb(108, 202, 159)
	if strings.Contains(currentStatus, "失败") || strings.Contains(currentStatus, "连接中") {
		statusColor = rgb(238, 181, 84)
	}
	savedDC, _, _ := procSaveDC.Call(hdc)
	procIntersectClip.Call(hdc, 272, 0, uintptr(bounds.Right-controlPanelWidth-8), titleBarHeight)
	drawText(hdc, 272, 9, currentStatus, statusColor, smallFont)
	procRestoreDC.Call(hdc, savedDC)

	width := bounds.Right
	clipLeft := int32(272)
	clipRight := width - controlPanelWidth - 8
	if clipRight < clipLeft+80 {
		clipRight = clipLeft + 80
	}
	savedDC, _, _ = procSaveDC.Call(hdc)
	procIntersectClip.Call(hdc, uintptr(clipLeft), titleBarHeight, uintptr(clipRight), uintptr(bounds.Bottom))

	baseText := messageText + "      "
	repeatedText := baseText + messageText
	if version != tickerMeasuredVersion {
		tickerSpan = measureText(hdc, baseText, tickerFont)
		tickerMeasuredVersion = version
		scrollOffset = 0
	}
	if tickerSpan > 0 {
		if scrollOffset >= float64(tickerSpan) {
			scrollOffset = 0
		}
		drawText(hdc, int(clipLeft)-int(scrollOffset), titleBarHeight+14, repeatedText, rgb(235, 239, 244), tickerFont)
	}
	procRestoreDC.Call(hdc, savedDC)

	controls := rect{Left: width - controlPanelWidth, Top: 0, Right: width, Bottom: bounds.Bottom}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&controls)), headerBrush)
	controlLabel := "暂停"
	if paused {
		controlLabel = "继续"
	}
	drawText(hdc, int(width)-220, 11, controlLabel, rgb(92, 207, 220), smallFont)
	drawText(hdc, int(width)-140, 11, "贴底", rgb(143, 160, 177), smallFont)
	drawText(hdc, int(width)-58, 8, "×", rgb(190, 202, 214), brandFont)
	drawText(hdc, int(width)-58, 31, "关闭", rgb(110, 125, 141), smallFont)
}

func drawText(hdc uintptr, x, y int, text string, color uint32, font uintptr) {
	if text == "" {
		return
	}
	encoded := utf16.Encode([]rune(text))
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	procSetTextColor.Call(hdc, uintptr(color))
	procSetBkMode.Call(hdc, transparent)
	procTextOutW.Call(
		hdc,
		uintptr(int32(x)),
		uintptr(int32(y)),
		uintptr(unsafe.Pointer(&encoded[0])),
		uintptr(len(encoded)),
	)
	procSelectObject.Call(hdc, oldFont)
}

func measureText(hdc uintptr, text string, font uintptr) int32 {
	encoded := utf16.Encode([]rune(text))
	if len(encoded) == 0 {
		return 0
	}
	var size point
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	procGetTextExtent.Call(
		hdc,
		uintptr(unsafe.Pointer(&encoded[0])),
		uintptr(len(encoded)),
		uintptr(unsafe.Pointer(&size)),
	)
	procSelectObject.Call(hdc, oldFont)
	return size.X
}

func rgb(r, g, b uint32) uint32 {
	return r | g<<8 | b<<16
}

func refreshNewsLoop() {
	client := &http.Client{Timeout: 12 * time.Second}
	for {
		refreshNews(client)
		time.Sleep(5 * time.Minute)
	}
}

func refreshNews(client *http.Client) {
	type result struct {
		items []newsItem
		err   error
	}
	results := make(chan result, len(feeds))
	for _, source := range feeds {
		source := source
		go func() {
			items, err := fetchFeed(client, source)
			results <- result{items: items, err: err}
		}()
	}

	combined := make([]newsItem, 0, 80)
	seen := make(map[string]bool)
	successes := 0
	for range feeds {
		response := <-results
		if response.err != nil {
			continue
		}
		successes++
		for _, item := range response.items {
			key := strings.ToLower(strings.TrimSpace(item.Title))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			combined = append(combined, item)
			if len(combined) == 80 {
				break
			}
		}
	}

	dataMu.Lock()
	if len(combined) > 0 {
		newsItems = combined
		parts := make([]string, 0, len(newsItems))
		for _, item := range newsItems {
			parts = append(parts, "["+item.Source+"] "+item.Title)
		}
		tickerText = strings.Join(parts, "      •      ")
		tickerVersion++
		statusText = fmt.Sprintf("LIVE  |  %d 条快讯  |  更新于 %s", len(newsItems), time.Now().Format("15:04"))
	} else if len(newsItems) > 0 {
		statusText = "连接失败  |  显示上次收到的新闻"
	} else if successes == 0 {
		statusText = "连接失败  |  请检查网络与 RSS 源"
		tickerText = "暂时无法获取实时新闻，请检查网络连接后稍候重试。"
		tickerVersion++
	} else {
		statusText = "连接中  |  RSS 暂无可显示新闻"
		tickerText = "新闻源暂未返回快讯，程序会自动重试。"
		tickerVersion++
	}
	dataMu.Unlock()
	if windowHandle != 0 {
		procInvalidateRect.Call(windowHandle, 0, 0)
	}
}

func fetchFeed(client *http.Client, source feed) ([]newsItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "NorthstarNewsTicker/1.0 (Windows desktop RSS reader)")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("RSS 请求返回 HTTP %d", response.StatusCode)
	}

	items, err := parseRSS(io.LimitReader(response.Body, 2<<20), source.source)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func parseRSS(reader io.Reader, category string) ([]newsItem, error) {
	var document rssDocument
	if err := xml.NewDecoder(reader).Decode(&document); err != nil {
		return nil, err
	}

	items := make([]newsItem, 0, len(document.Channel.Items))
	for _, entry := range document.Channel.Items {
		title := strings.Join(strings.Fields(html.UnescapeString(entry.Title)), " ")
		if title == "" {
			continue
		}
		items = append(items, newsItem{Source: category, Title: title})
	}
	return items, nil
}

func showError(err error) {
	message, _ := syscall.UTF16PtrFromString(err.Error())
	title, _ := syscall.UTF16PtrFromString("Northstar News Ticker")
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
