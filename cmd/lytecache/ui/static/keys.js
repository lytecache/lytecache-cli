// Keyboard shortcuts: "/" focuses search, arrow keys move a row
// selection, Enter opens the selected row, Esc clears search or the
// selection. Also wires data-confirm on forms to a native confirm()
// dialog for destructive actions.
(function () {
  "use strict";

  function isTypingTarget(el) {
    return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA");
  }

  function focusSearch(e) {
    if (e.key !== "/" || isTypingTarget(e.target)) return;
    var el = document.getElementById("key-filter") || document.getElementById("global-search");
    if (!el) return;
    e.preventDefault();
    el.focus();
    el.select();
  }

  function rows() {
    return Array.prototype.slice.call(document.querySelectorAll("tr[data-key-row]"));
  }

  function selectedIndex(list) {
    for (var i = 0; i < list.length; i++) {
      if (list[i].classList.contains("selected")) return i;
    }
    return -1;
  }

  function selectRow(list, index) {
    list.forEach(function (r) { r.classList.remove("selected"); });
    if (index < 0 || index >= list.length) return;
    list[index].classList.add("selected");
    list[index].scrollIntoView({ block: "nearest" });
    list[index].focus();
  }

  function handleArrowsAndEnter(e) {
    if (isTypingTarget(e.target)) return;
    var list = rows();
    if (!list.length) return;
    var idx = selectedIndex(list);

    if (e.key === "ArrowDown") {
      e.preventDefault();
      selectRow(list, idx < 0 ? 0 : Math.min(idx + 1, list.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      selectRow(list, idx < 0 ? 0 : Math.max(idx - 1, 0));
    } else if (e.key === "Enter" && idx >= 0) {
      var href = list[idx].getAttribute("data-href");
      if (href) window.location = href;
    }
  }

  function handleEscape(e) {
    if (e.key !== "Escape") return;
    var active = document.activeElement;
    if (active && (active.id === "global-search" || active.id === "key-filter")) {
      active.value = "";
      active.blur();
      return;
    }
    rows().forEach(function (r) { r.classList.remove("selected"); });
    if (isTypingTarget(active)) active.blur();
  }

  document.addEventListener("keydown", function (e) {
    focusSearch(e);
    handleArrowsAndEnter(e);
    handleEscape(e);
  });

  document.addEventListener("submit", function (e) {
    var msg = e.target.getAttribute("data-confirm");
    if (msg && !window.confirm(msg)) {
      e.preventDefault();
    }
  });
})();
