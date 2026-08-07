// Collapsible, syntax-highlighted JSON tree for the value viewer. No
// library -- a small recursive DOM builder over the value the server
// already rendered as pretty-printed JSON text inside
// <pre class="json-value" data-json>.
(function () {
  "use strict";

  function leaf(text, cls) {
    var span = document.createElement("span");
    span.className = cls;
    span.textContent = text;
    return span;
  }

  function buildNode(value) {
    if (value === null) return leaf("null", "json-literal");
    switch (typeof value) {
      case "string":
        return leaf(JSON.stringify(value), "json-string");
      case "number":
        return leaf(String(value), "json-number");
      case "boolean":
        return leaf(String(value), "json-literal");
      case "object":
        return Array.isArray(value)
          ? buildContainer(value.map(function (v, i) { return [i, v]; }), "[", "]", true)
          : buildContainer(Object.keys(value).map(function (k) { return [k, value[k]]; }), "{", "}", false);
      default:
        return leaf(String(value), "json-literal");
    }
  }

  function buildContainer(entries, open, close, isArray) {
    if (entries.length === 0) {
      return leaf(open + close, "json-literal");
    }

    var details = document.createElement("details");
    details.open = true;
    var summary = document.createElement("summary");
    summary.textContent = open + " " + entries.length + " " + (isArray ? "item(s)" : "field(s)") + " " + close;
    details.appendChild(summary);

    entries.forEach(function (pair) {
      var row = document.createElement("div");
      if (!isArray) {
        row.appendChild(leaf(JSON.stringify(String(pair[0])) + ": ", "json-key"));
      }
      row.appendChild(buildNode(pair[1]));
      details.appendChild(row);
    });
    return details;
  }

  document.querySelectorAll("pre.json-value[data-json]").forEach(function (pre) {
    var parsed;
    try {
      parsed = JSON.parse(pre.textContent);
    } catch (e) {
      return; // leave the raw pretty-printed text as-is if it doesn't parse
    }
    var tree = document.createElement("div");
    tree.className = "json-tree";
    tree.appendChild(buildNode(parsed));
    pre.replaceWith(tree);
  });
})();
