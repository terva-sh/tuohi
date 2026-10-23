# Navigation and network requests

`View.Origins` decides which top-level documents may load in the view and which
origins may call its Go bridge. It does not restrict all traffic from the browser
engine. Cross-origin frames and subresources remain usable; their access to the
bridge is independently denied. Consumers needing a network egress boundary must
provide one outside this navigation policy.

The trusted-document bridge intercepts ordinary outside link clicks, GET forms
and supported Navigation API events before asking the engine to navigate. Go
reapplies navigation policy before handing the URL to `App.OpenExternal` or the
system browser. A server redirect, meta refresh or navigation the intercept does
not see can still reach the native policy. Refusal prevents an untrusted document
from replacing the trusted page; it does not undo a request already sent.

## Windows contract

Microsoft's
[`ICoreWebView2NavigationStartingEventArgs.get_Cancel`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2navigationstartingeventargs#get_cancel)
documentation states: “For performance reasons, GET HTTP requests may happen,
while the host is responding.” Cancellation keeps the current page intact and
completes the navigation with `OPERATION_CANCELED`. This explains why a cancelled
navigation can contact its destination; it is allowed by the API, rather than a
guarantee specific to an origin, runtime version or prefetch header. It does not
mean every cancelled navigation sends a request.

The navigation-starting event-args interface has no `GetDeferral`. A deferral is
available on
[`WebResourceRequested`](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2#add_webresourcerequested),
which can hold matching resource requests before they proceed. That is a
different policy surface. Applying an untrusted-origin block to all documents or
resources would also block cross-origin iframes or assets the current API permits.
Its document resource context alone does not establish that a request belongs to
the top-level frame. The older `AddWebResourceRequestedFilter` used for tuohi's
app-content virtual host also has documented iframe limitations; it is not a
blanket request firewall.

Tuohi therefore retains navigation cancellation and its early page intercept,
rather than changing frame/subresource behavior under an existing setting. A
future request-filter API needs its own explicit contract, main-frame/source
identification, runtime compatibility checks and native tests. Blocking all
foreign resources, silently adding a request delay, or treating a navigation
deferral as available would not meet the present API's compatibility contract.

On Linux, the main-frame response policy likewise runs after the request. macOS
uses its action policy before the initial request and judges redirect responses
again. None of these top-level navigation policies constrains all iframe,
subresource, fetch, worker or socket traffic.

## Evidence and regressions

The original Windows observations in GitHub runs
[36512765547](https://github.com/terva-sh/tuohi/actions/runs/36512765547) and
[36513098161](https://github.com/terva-sh/tuohi/actions/runs/36513098161) used
WebView2 runtime `153.0.4234.48`: cancelled navigation reached a second loopback
origin as ordinary GET requests, without prefetch headers. The documented API
contract explains why changing origin spelling or assuming a newer runtime fixes
the limit is insufficient.

At tuohi `68fc3ac7539712deb1cabe405142b38d317e67bb`, native Windows
[job 120154764292](https://github.com/terva-sh/tuohi/actions/runs/39873829324/job/120154764292)
passed these existing meaningful regressions with cgo disabled:

- `TestNavigationPolicy`: a reachable outside server redirect leaves the trusted
  page intact, hands the destination to the external opener, and still permits
  an untrusted frame to run without the page's bridge privilege.
- `TestOutsideLinksNotRequested`: intercepted link/script navigations produce no
  requests at the recording outside server. Its redirect case uses an
  unresolvable host; it is not evidence that reachable redirects send no request.
- `TestOriginGate` and `TestFrameGate`: untrusted documents and frames cannot
  exercise the trusted Go bindings.

The external opener is an App-scoped test hook, so these tests do not start a real
browser that could make its own request. Preserve the distinct assertions:
document retention, external handoff, bridge isolation and early-intercept request
suppression. Do not replace them with an assertion that every refused native
navigation generates zero traffic; the native API does not promise that.

Investigation: `TKT-01M3NGAJ7SCY3WM0THV1KAMS4Y` (Keep WebView2 from requesting
a navigation NavigationStarting cancels). The accepted outcome is documenting
the limit and its reason; this investigation changes no runtime behavior.
