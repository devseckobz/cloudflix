import {test} from 'node:test'
import assert from 'node:assert/strict'
import {browserLabel,clientDetail,describeSession} from '../src/lib/user-agent.js'

// Real User-Agent strings, as recorded by the server (capped at 256 bytes).
const UA = {
 chromeMac: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
 chromeWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
 safariMac: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Safari/605.1.15',
 safariIPhone: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1',
 safariIPad: 'Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1',
 chromeAndroid: 'Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36',
 chromeIos: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/124.0.6367.88 Mobile/15E148 Safari/604.1',
 firefoxLinux: 'Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0',
 firefoxWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:134.0) Gecko/20100101 Firefox/134.0',
 edgeWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0',
 edgeAndroid: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36 EdgA/131.0.0.0',
 operaWindows: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 OPR/115.0.0.0',
 samsungAndroid: 'Mozilla/5.0 (Linux; Android 13; SM-G991B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36',
 androidWebview: 'Mozilla/5.0 (Linux; Android 13; Pixel 7 Build/TQ3A.230805.001; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/131.0.0.0 Mobile Safari/537.36',
 chromeos: 'Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36',
 curl: 'curl/8.5.0',
 empty: '',
}

test('the most specific Chromium fork wins over the Chrome and Safari tokens',()=>{
 assert.equal(browserLabel(UA.edgeWindows),'Edge 131 · Windows')
 assert.equal(browserLabel(UA.edgeAndroid),'Edge 131 · Android 14')
 assert.equal(browserLabel(UA.operaWindows),'Opera 115 · Windows')
 assert.equal(browserLabel(UA.samsungAndroid),'Samsung Internet 23 · Android 13')
 // Plain Chrome must not be mistaken for Safari even though it claims Safari/.
 assert.equal(browserLabel(UA.chromeWindows),'Chrome 131 · Windows')
 assert.equal(browserLabel(UA.chromeMac),'Chrome 131 · macOS')
 assert.equal(browserLabel(UA.chromeos),'Chrome 131 · ChromeOS')
})

test('browsers report a major version',()=>{
 assert.equal(browserLabel(UA.firefoxLinux),'Firefox 133 · Linux')
 assert.equal(browserLabel(UA.firefoxWindows),'Firefox 134 · Windows')
 assert.equal(browserLabel(UA.safariMac),'Safari 18 · macOS')
 assert.equal(browserLabel(UA.curl),'curl 8')
})

test('iPad and iPhone are told apart, with the iOS version',()=>{
 // Both used to render as the identical "Safari · iOS".
 assert.equal(browserLabel(UA.safariIPhone),'Safari 18 · iOS 18.1')
 assert.equal(browserLabel(UA.safariIPad),'Safari 17 · iPadOS 17.5')
 assert.notEqual(browserLabel(UA.safariIPhone),browserLabel(UA.safariIPad))
})

test('an Android WebView is not reported as ordinary Chrome',()=>{
 const webview=describeSession(UA.androidWebview)
 const chrome=describeSession(UA.chromeAndroid)
 assert.equal(webview.browser,'Chrome')
 // The marker lives in the detail line so the row stays short, and the model
 // is still reported next to it.
 assert.equal(webview.device,'WebView · Pixel 7')
 assert.notEqual(webview.device,chrome.device)
 assert.equal(chrome.device,'SM-S918B')
 // An ordinary Chrome-for-Android row never claims to be a WebView.
 assert.doesNotMatch(chrome.device,/WebView/)
})

test('iOS Chrome is Chrome, not Safari',()=>{
 assert.equal(browserLabel(UA.chromeIos),'Chrome 124 · iOS 17.4')
})

test('Android version comes from the Android token, not "Linux"',()=>{
 assert.equal(describeSession(UA.chromeAndroid).platform,'Android 14')
 assert.equal(describeSession(UA.firefoxLinux).platform,'Linux')
})

test('empty and unknown agents degrade without throwing',()=>{
 assert.equal(browserLabel(UA.empty),'Unknown browser')
 assert.equal(browserLabel('something-totally-unknown'),'Unknown browser')
 assert.equal(describeSession('').initial,'U')
 assert.equal(clientDetail(UA.empty),'')
 assert.equal(browserLabel(undefined),'Unknown browser')
})

test('detail only carries information the label does not already show',()=>{
 // macOS version is the sole extra for a desktop Mac.
 assert.equal(clientDetail(UA.chromeMac),'macOS 10.15.7')
 assert.equal(clientDetail(UA.chromeWindows),'')
 assert.equal(clientDetail(UA.firefoxLinux),'')
 assert.equal(clientDetail(UA.safariIPad),'')
})

test('avatar initial is the browser letter, uppercased',()=>{
 assert.equal(describeSession(UA.edgeWindows).initial,'E')
 assert.equal(describeSession(UA.curl).initial,'C')
 assert.equal(describeSession(UA.firefoxLinux).initial,'F')
})
