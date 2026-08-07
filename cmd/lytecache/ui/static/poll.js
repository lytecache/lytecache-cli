// Auto-refresh: fetches the current page in the background on an interval
// (see body's data-refresh-seconds, set only on the dashboard) and swaps
// in just #refresh-region, instead of reloading the whole page -- a full
// reload was interrupting whatever the operator was doing (scrolling,
// about to click, mid-selection) every couple of seconds. The server
// still computes every value (health flags, hit rates, etc.); this only
// replaces the one region's markup with what the server just rendered,
// so there's no client-side merge logic to get wrong. A
// localStorage-persisted pause toggle (#auto-refresh checkbox) still
// works exactly as before.
(function () {
  "use strict";

  var seconds = parseInt(document.body.getAttribute("data-refresh-seconds") || "0", 10);
  if (!seconds) return;

  var storageKey = "lytecache-ui-autorefresh-paused";
  var toggle = document.getElementById("auto-refresh");
  var regionId = "refresh-region";

  function isPaused() {
    try {
      return window.localStorage.getItem(storageKey) === "1";
    } catch (e) {
      return false; // localStorage unavailable (private mode etc.) -- default to refreshing
    }
  }

  function setPaused(paused) {
    try {
      window.localStorage.setItem(storageKey, paused ? "1" : "0");
    } catch (e) {
      // ignore -- toggle still works for this page load, just doesn't persist
    }
  }

  if (toggle) {
    toggle.checked = !isPaused();
    toggle.addEventListener("change", function () {
      setPaused(!toggle.checked);
    });
  }

  function refresh() {
    if (isPaused()) return;
    var currentRegion = document.getElementById(regionId);
    if (!currentRegion) return;

    fetch(window.location.href, { credentials: "same-origin" })
      .then(function (res) { return res.text(); })
      .then(function (html) {
        var next = new DOMParser().parseFromString(html, "text/html");
        var nextRegion = next.getElementById(regionId);
        // A missing region most likely means the session expired and this
        // fetch landed on the login page instead -- leave the visible page
        // alone; the operator's next real click will hit the login
        // redirect the normal way rather than getting silently swapped
        // out from under them mid-background-refresh.
        if (!nextRegion) return;
        currentRegion.replaceWith(nextRegion);
      })
      .catch(function () {
        // Network blip or similar -- just try again on the next tick.
      });
  }

  setInterval(refresh, seconds * 1000);
})();
