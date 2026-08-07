// Live TTL countdown: a server-rendered duration string goes stale the
// instant the page finishes loading, so every [data-expires-at] element
// (an absolute ISO instant) is recomputed client-side on an interval.
(function () {
  "use strict";

  function format(ms) {
    if (ms <= 0) return "expired";
    var totalSeconds = Math.floor(ms / 1000);
    var h = Math.floor(totalSeconds / 3600);
    var m = Math.floor((totalSeconds % 3600) / 60);
    var s = totalSeconds % 60;
    var parts = [];
    if (h) parts.push(h + "h");
    if (h || m) parts.push(m + "m");
    parts.push(s + "s");
    return parts.join("");
  }

  function tick() {
    var now = Date.now();
    document.querySelectorAll("[data-expires-at]").forEach(function (el) {
      var expires = Date.parse(el.getAttribute("data-expires-at"));
      if (isNaN(expires)) return;
      el.textContent = format(expires - now);
    });
  }

  if (document.querySelector("[data-expires-at]")) {
    tick();
    setInterval(tick, 1000);
  }
})();
