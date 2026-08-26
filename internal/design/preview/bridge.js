// Pando design preview bridge.
//
// Injected only into ?bridge=1 requests coming from the Pando UI, so an
// exported or directly opened artifact stays exactly the markup the agent
// wrote. It adds three things and nothing else:
//
//   - hover outline over any element carrying data-pando-id
//   - click-to-select, reported to the parent frame as design://<node_id>
//   - slide navigation for decks, driven from the parent or the keyboard
//
// It never mutates the document beyond its own overlay: v1 is select-and-ask,
// not direct manipulation.
(function () {
  "use strict";

  var ORIGIN_MESSAGE = "pando-design";
  var selected = null;

  function post(type, payload) {
    var message = { source: ORIGIN_MESSAGE, type: type };
    for (var key in payload) {
      if (Object.prototype.hasOwnProperty.call(payload, key)) {
        message[key] = payload[key];
      }
    }
    // The parent is the Pando UI on the same origin; "*" keeps this working
    // when the preview is opened standalone, where there is no parent to talk to.
    try {
      window.parent.postMessage(message, "*");
    } catch (err) {
      /* standalone window: nothing to report to */
    }
  }

  function style() {
    var css = document.createElement("style");
    css.id = "pando-bridge-style";
    css.textContent =
      "[data-pando-hover]{outline:2px dashed rgba(59,130,246,.9)!important;outline-offset:1px!important;cursor:pointer!important}" +
      "[data-pando-selected]{outline:2px solid rgba(59,130,246,1)!important;outline-offset:1px!important}";
    document.head.appendChild(css);
  }

  function target(node) {
    while (node && node !== document.documentElement) {
      if (node.getAttribute && node.getAttribute("data-pando-id")) return node;
      node = node.parentNode;
    }
    return null;
  }

  function describe(el) {
    var box = el.getBoundingClientRect();
    return {
      nodeId: el.getAttribute("data-pando-id"),
      selection: "design://" + el.getAttribute("data-pando-id"),
      tag: el.tagName.toLowerCase(),
      text: (el.textContent || "").trim().slice(0, 120),
      slide: slideOf(el),
      box: { x: box.left, y: box.top, w: box.width, h: box.height },
    };
  }

  function slideOf(el) {
    var node = el;
    while (node && node !== document.documentElement) {
      if (node.hasAttribute && node.hasAttribute("data-pando-slide")) {
        return parseInt(node.getAttribute("data-pando-slide"), 10) || 0;
      }
      node = node.parentNode;
    }
    return 0;
  }

  function select(el) {
    if (selected) selected.removeAttribute("data-pando-selected");
    selected = el;
    if (el) {
      el.setAttribute("data-pando-selected", "");
      post("selected", describe(el));
    } else {
      post("selected", { nodeId: "", selection: "" });
    }
  }

  document.addEventListener(
    "mouseover",
    function (event) {
      var el = target(event.target);
      if (!el) return;
      el.setAttribute("data-pando-hover", "");
    },
    true
  );

  document.addEventListener(
    "mouseout",
    function (event) {
      var el = target(event.target);
      if (el) el.removeAttribute("data-pando-hover");
    },
    true
  );

  document.addEventListener(
    "click",
    function (event) {
      var el = target(event.target);
      if (!el) return;
      // A preview click means "tell the agent about this element", never
      // "follow this link" — navigating away would lose the selection.
      event.preventDefault();
      event.stopPropagation();
      select(el);
    },
    true
  );

  function slides() {
    return document.querySelectorAll("[data-pando-slide]");
  }

  function goToSlide(index) {
    var all = slides();
    if (!all.length) return;
    var clamped = Math.max(1, Math.min(index, all.length));
    var el = document.querySelector('[data-pando-slide="' + clamped + '"]');
    if (el) el.scrollIntoView({ behavior: "smooth", block: "start" });
    post("slide", { slide: clamped, slides: all.length });
  }

  window.addEventListener("message", function (event) {
    var data = event.data;
    if (!data || data.source !== ORIGIN_MESSAGE) return;
    if (data.type === "goToSlide") goToSlide(data.slide);
    if (data.type === "select") {
      var el = document.querySelector('[data-pando-id="' + data.nodeId + '"]');
      if (el) {
        select(el);
        el.scrollIntoView({ behavior: "smooth", block: "center" });
      }
    }
    if (data.type === "clearSelection") select(null);
  });

  function ready() {
    style();
    // A deck URL carries its slide in the fragment (#slide-3). The artifact has
    // no anchor of that name, so the bridge resolves it against the stamped
    // slide index instead of relying on the browser's own jump.
    var deep = /^#slide-(\d+)$/.exec(location.hash || "");
    if (deep) goToSlide(parseInt(deep[1], 10));
    post("ready", {
      title: document.title,
      slides: slides().length,
      nodes: document.querySelectorAll("[data-pando-id]").length,
      url: location.pathname,
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", ready);
  } else {
    ready();
  }
})();
