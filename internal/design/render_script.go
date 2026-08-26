package design

// indexScript walks the rendered document, stamps every meaningful element with
// a stable data-pando-id, and returns the structure index the inspector and the
// UI selection protocol both work from.
//
// It runs in the page, so it mutates the live DOM only: the artifact files on
// disk are never rewritten by a render.
const indexScript = `(function (opts) {
  var SKIP = { SCRIPT: 1, STYLE: 1, META: 1, LINK: 1, HEAD: 1, TITLE: 1, BASE: 1, NOSCRIPT: 1, TEMPLATE: 1 };
  var slides = opts.slideSelector ? Array.prototype.slice.call(document.querySelectorAll(opts.slideSelector)) : [];
  var nodes = [];
  var facts = [];
  var counter = 0;
  var truncated = false;

  function esc(value) {
    return (window.CSS && CSS.escape) ? CSS.escape(value) : value.replace(/([^\w-])/g, '\\$1');
  }

  function selectorFor(el) {
    var parts = [];
    var current = el;
    while (current && current.nodeType === 1 && current !== document.documentElement) {
      if (current.id) {
        parts.unshift('#' + esc(current.id));
        break;
      }
      var tag = current.tagName.toLowerCase();
      var parent = current.parentElement;
      if (parent) {
        var same = 0, index = 0;
        for (var i = 0; i < parent.children.length; i++) {
          var sibling = parent.children[i];
          if (sibling.tagName !== current.tagName) continue;
          same++;
          if (sibling === current) index = same;
        }
        if (same > 1) tag += ':nth-of-type(' + index + ')';
      }
      parts.unshift(tag);
      if (parts.length >= 6) break;
      current = current.parentElement;
    }
    return parts.join(' > ');
  }

  function ownText(el) {
    var text = '';
    for (var i = 0; i < el.childNodes.length; i++) {
      var child = el.childNodes[i];
      if (child.nodeType === 3) text += child.nodeValue;
    }
    text = text.replace(/\s+/g, ' ').trim();
    return text.length > 160 ? text.slice(0, 160) + '…' : text;
  }

  function slideOf(el) {
    for (var i = 0; i < slides.length; i++) {
      if (slides[i] === el || slides[i].contains(el)) return i;
    }
    return 0;
  }

  function stylesOf(el) {
    var computed = window.getComputedStyle(el);
    var out = {};
    for (var i = 0; i < opts.styleProps.length; i++) {
      var prop = opts.styleProps[i];
      var value = computed.getPropertyValue(prop);
      if (value) out[prop] = value.trim();
    }
    return out;
  }

  var INTERACTIVE_TAGS = { A: 1, BUTTON: 1, INPUT: 1, SELECT: 1, TEXTAREA: 1, SUMMARY: 1 };
  var INTERACTIVE_ROLES = {
    button: 1, link: 1, checkbox: 1, radio: 1, 'switch': 1,
    tab: 1, menuitem: 1, menuitemcheckbox: 1, option: 1, textbox: 1, slider: 1
  };
  var TRANSPARENT = /^rgba\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*,\s*0\s*\)$/;

  function labelText(el) {
    var labelledby = el.getAttribute('aria-labelledby');
    if (!labelledby) return '';
    var parts = [];
    var ids = labelledby.split(/\s+/);
    for (var i = 0; i < ids.length; i++) {
      var ref = document.getElementById(ids[i]);
      if (ref) parts.push((ref.textContent || '').replace(/\s+/g, ' ').trim());
    }
    return parts.join(' ').trim();
  }

  // accName is a deliberate approximation of the accessible-name computation:
  // enough to tell a named control from an unnamed one, which is the only
  // question the audit asks of it.
  function accName(el) {
    var aria = (el.getAttribute('aria-label') || '').trim();
    if (aria) return aria;
    var labelled = labelText(el);
    if (labelled) return labelled;

    var tag = el.tagName.toLowerCase();
    var alt = el.getAttribute('alt');
    if (alt !== null && alt.trim()) return alt.trim();
    if (tag === 'input' || tag === 'select' || tag === 'textarea') {
      if (el.id) {
        var explicit = document.querySelector('label[for="' + esc(el.id) + '"]');
        if (explicit && explicit.textContent.trim()) return explicit.textContent.replace(/\s+/g, ' ').trim();
      }
      var wrapping = el.closest ? el.closest('label') : null;
      if (wrapping && wrapping.textContent.trim()) return wrapping.textContent.replace(/\s+/g, ' ').trim();
      var value = (el.getAttribute('value') || '').trim();
      if (value) return value;
      var placeholder = (el.getAttribute('placeholder') || '').trim();
      if (placeholder) return placeholder;
    }
    var text = (el.textContent || '').replace(/\s+/g, ' ').trim();
    if (text) return text.length > 80 ? text.slice(0, 80) : text;
    return (el.getAttribute('title') || '').trim();
  }

  // effectiveBackground walks up to the first ancestor that actually paints,
  // because contrast is measured against what is behind the text, not against
  // the transparent background most elements declare.
  function effectiveBackground(el) {
    var current = el;
    while (current && current.nodeType === 1) {
      var bg = window.getComputedStyle(current).backgroundColor;
      if (bg && bg !== 'transparent' && !TRANSPARENT.test(bg)) return bg;
      current = current.parentElement;
    }
    return 'rgb(255, 255, 255)';
  }

  function headingLevel(el) {
    var tag = el.tagName.toLowerCase();
    if (/^h[1-6]$/.test(tag)) return parseInt(tag.slice(1), 10);
    if ((el.getAttribute('role') || '') === 'heading') {
      var level = parseInt(el.getAttribute('aria-level') || '0', 10);
      return isNaN(level) ? 0 : level;
    }
    return 0;
  }

  function isInteractive(el) {
    var role = el.getAttribute('role') || '';
    if (INTERACTIVE_ROLES[role]) return true;
    if (el.tagName === 'A') return el.hasAttribute('href');
    if (INTERACTIVE_TAGS[el.tagName]) return true;
    return el.hasAttribute('onclick');
  }

  function isAriaHidden(el) {
    var current = el;
    while (current && current.nodeType === 1) {
      if (current.getAttribute('aria-hidden') === 'true') return true;
      current = current.parentElement;
    }
    return false;
  }

  function factsFor(el, id, text) {
    var tag = el.tagName.toLowerCase();
    var level = headingLevel(el);
    var interactive = isInteractive(el);
    // Facts exist to answer audit rules; an element no rule can fire on is
    // payload with no reader.
    if (!level && !interactive && tag !== 'img' && !text) return null;
    var computed = window.getComputedStyle(el);
    return {
      node_id: id,
      tag: tag,
      name: accName(el),
      alt_present: el.hasAttribute('alt'),
      heading_level: level,
      interactive: interactive,
      aria_hidden: isAriaHidden(el),
      has_text: !!text,
      color: computed.color || '',
      background: effectiveBackground(el),
      font_size: parseFloat(computed.fontSize) || 0,
      font_weight: parseInt(computed.fontWeight, 10) || 400
    };
  }

  function walk(el, depth, parentID) {
    if (depth > opts.maxDepth) return;
    if (nodes.length >= opts.maxNodes) { truncated = true; return; }
    if (SKIP[el.tagName]) return;

    var rect = el.getBoundingClientRect();
    var text = ownText(el);
    var visible = rect.width > 0 || rect.height > 0;
    var id = parentID;

    if (visible || text) {
      id = 'n' + (++counter);
      el.setAttribute('data-pando-id', id);
      nodes.push({
        node_id: id,
        parent_id: parentID,
        selector: selectorFor(el),
        role: el.getAttribute('role') || el.tagName.toLowerCase(),
        text: text,
        slide: slideOf(el),
        box: {
          x: rect.left + window.scrollX,
          y: rect.top + window.scrollY,
          w: rect.width,
          h: rect.height
        },
        styles: stylesOf(el)
      });
      var nodeFacts = factsFor(el, id, text);
      if (nodeFacts) facts.push(nodeFacts);
    }

    for (var i = 0; i < el.children.length; i++) {
      walk(el.children[i], depth + 1, id);
    }
  }

  if (document.body) walk(document.body, 0, '');

  return {
    title: document.title || '',
    slides: slides.length,
    truncated: truncated,
    scroll_height: document.documentElement ? document.documentElement.scrollHeight : 0,
    scroll_width: document.documentElement ? document.documentElement.scrollWidth : 0,
    nodes: nodes,
    facts: facts
  };
})(%s)`

// slideRectScript returns the document-space box of one slide, used to clip a
// per-slide screenshot.
const slideRectScript = `(function (selector, index) {
  var slides = document.querySelectorAll(selector);
  if (index < 0 || index >= slides.length) return null;
  var rect = slides[index].getBoundingClientRect();
  return { x: rect.left + window.scrollX, y: rect.top + window.scrollY, w: rect.width, h: rect.height };
})(%s, %d)`

// printBreakScript reports, under print emulation, whether each slide is
// actually followed by a page break. Deck PDF export prints one slide per page,
// which only works when the deck ships print styles; this is what lets a test
// (and later the critic) catch a deck that does not.
const printBreakScript = `(function (selector) {
  var slides = document.querySelectorAll(selector);
  var out = [];
  for (var i = 0; i < slides.length; i++) {
    var style = window.getComputedStyle(slides[i]);
    out.push({
      index: i,
      break_after: style.getPropertyValue('break-after') || style.getPropertyValue('page-break-after') || '',
      height: slides[i].getBoundingClientRect().height
    });
  }
  return out;
})(%s)`
