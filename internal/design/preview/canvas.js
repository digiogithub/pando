// The design canvas: every artifact of a session as an artboard on one
// pan-and-zoom surface, refreshed while the agent works.
//
// It is a viewer, never an editor. Each artboard frames the very document the
// preview server serves, so what the user sees is the file on disk — but a
// transparent capture layer takes every pointer event, so the only things the
// user can do are look, pan, zoom and focus. Editing stays with the agent.
//
// State arrives by polling `artboards` rather than over a socket: the payload is
// a handful of rows, the page must work in both the mounted and the loopback
// deployment, and each artboard's own live-reload script already refreshes its
// content. The poll is here for the canvas *layout* — boards appearing,
// disappearing, changing size or version.

(function () {
  'use strict';

  var POLL_MS = 1000;
  var GAP = 96;                 // space between artboards, in canvas units
  var LABEL_H = 26;             // height of an artboard's label strip
  var ROW_MAX = 4600;           // wrap a row once it passes this width
  var MIN_ZOOM = 0.02;
  var MAX_ZOOM = 4;

  var viewport = document.getElementById('viewport');
  var world = document.getElementById('world');
  var zoomLabel = document.getElementById('zoom');
  var jump = document.getElementById('jump');
  var followBtn = document.getElementById('follow');
  var dot = document.getElementById('dot');
  var liveText = document.getElementById('live-text');
  var empty = document.getElementById('empty');
  var activityList = document.getElementById('activity-list');

  // view is the canvas transform: world coordinates -> screen.
  var view = { x: 0, y: 0, k: 1 };
  var boards = new Map();       // id -> {data, el, iframe, box}
  var follow = true;
  var userPlacedView = false;   // true once the user pans or zooms by hand
  var failures = 0;

  // ---------------------------------------------------------------- render

  function applyView() {
    world.style.transform = 'translate(' + view.x + 'px,' + view.y + 'px) scale(' + view.k + ')';
    zoomLabel.textContent = Math.round(view.k * 100) + '%';
  }

  function boardSize(data) {
    var w = data.width > 0 ? data.width : 1440;
    var h = data.height > 0 ? data.height : 900;
    return { w: w, h: h };
  }

  // layout packs the artboards into wrapping rows, largest row height driving
  // the next row's offset. It is deterministic: the same list always lands in
  // the same place, so an artboard never jumps while the user is reading it.
  function layout(list) {
    var x = 0, y = 0, rowH = 0;
    list.forEach(function (data) {
      var size = boardSize(data);
      if (x > 0 && x + size.w > ROW_MAX) {
        x = 0;
        y += rowH + GAP + LABEL_H;
        rowH = 0;
      }
      data._x = x;
      data._y = y;
      x += size.w + GAP;
      rowH = Math.max(rowH, size.h);
    });
  }

  function worldBounds() {
    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    boards.forEach(function (entry) {
      var size = boardSize(entry.data);
      minX = Math.min(minX, entry.data._x);
      minY = Math.min(minY, entry.data._y - LABEL_H);
      maxX = Math.max(maxX, entry.data._x + size.w);
      maxY = Math.max(maxY, entry.data._y + size.h);
    });
    if (minX === Infinity) return null;
    return { x: minX, y: minY, w: maxX - minX, h: maxY - minY };
  }

  function fitBox(box, padding) {
    if (!box) return;
    var pad = padding === undefined ? 80 : padding;
    var vw = viewport.clientWidth - pad * 2;
    var vh = viewport.clientHeight - pad * 2;
    if (vw <= 0 || vh <= 0) return;
    var k = Math.min(vw / box.w, vh / box.h);
    view.k = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, k));
    view.x = pad + (vw - box.w * view.k) / 2 - box.x * view.k;
    view.y = pad + (vh - box.h * view.k) / 2 - box.y * view.k;
    applyView();
  }

  function fitAll() {
    fitBox(worldBounds());
  }

  function focusBoard(id, padding) {
    var entry = boards.get(id);
    if (!entry) return;
    var size = boardSize(entry.data);
    fitBox({ x: entry.data._x, y: entry.data._y - LABEL_H, w: size.w, h: size.h + LABEL_H }, padding);
  }

  // ---------------------------------------------------------------- boards

  function buildBoard(data) {
    var el = document.createElement('section');
    el.className = 'board';

    var label = document.createElement('div');
    label.className = 'label';

    var name = document.createElement('span');
    name.className = 'name';
    label.appendChild(name);

    var meta = document.createElement('span');
    meta.className = 'meta';
    label.appendChild(meta);

    var badge = document.createElement('span');
    badge.className = 'badge';
    badge.hidden = true;
    label.appendChild(badge);

    var open = document.createElement('button');
    open.className = 'open';
    open.textContent = 'open ↗';
    open.title = 'Open this artifact on its own, in a new tab';
    label.appendChild(open);

    var frame = document.createElement('div');
    frame.className = 'frame';

    var iframe = document.createElement('iframe');
    iframe.setAttribute('loading', 'lazy');
    iframe.setAttribute('title', data.title || data.id);
    // No sandbox attribute, deliberately: the framed document is same-origin
    // so its injected live-reload poll can reach the token-scoped `_live`
    // endpoint, which is what makes an artboard refresh itself the moment the
    // agent rewrites the file. The artifact CSP is what confines it, and the
    // capture layer above is what keeps the user from editing it.
    frame.appendChild(iframe);

    var capture = document.createElement('div');
    capture.className = 'capture';
    frame.appendChild(capture);

    var fail = document.createElement('div');
    fail.className = 'fail';
    fail.hidden = true;
    frame.appendChild(fail);

    el.appendChild(label);
    el.appendChild(frame);
    world.appendChild(el);

    var entry = {
      data: data, el: el, iframe: iframe, name: name, meta: meta,
      badge: badge, fail: fail, open: open, loaded: ''
    };
    open.addEventListener('click', function (event) {
      event.stopPropagation();
      if (entry.data.url) window.open(entry.data.url, '_blank', 'noopener');
    });
    // Double-clicking anywhere on the board frames it. The capture layer is
    // above the document, so this works without the click ever reaching it.
    el.addEventListener('dblclick', function () {
      userPlacedView = true;
      focusBoard(entry.data.id, 40);
    });
    return entry;
  }

  function paintBoard(entry) {
    var data = entry.data;
    var size = boardSize(data);
    entry.el.style.left = data._x + 'px';
    entry.el.style.top = (data._y - LABEL_H) + 'px';
    entry.el.style.width = size.w + 'px';

    entry.name.textContent = data.title || data.slug || data.id;
    var bits = [];
    if (data.kind) bits.push(data.kind);
    bits.push(size.w + '×' + size.h);
    if (data.version > 0) bits.push('v' + data.version);
    entry.meta.textContent = bits.join(' · ');

    var status = data.status || 'ready';
    if (status === 'ready') {
      entry.badge.hidden = true;
    } else {
      entry.badge.hidden = false;
      entry.badge.className = 'badge ' + status;
      entry.badge.textContent = status === 'building' ? 'building…' : status;
      entry.badge.title = data.note || '';
    }

    entry.iframe.style.width = size.w + 'px';
    entry.iframe.style.height = size.h + 'px';

    if (!data.url) {
      entry.fail.hidden = false;
      entry.fail.textContent = data.note || 'This artifact has not been rendered yet.';
      entry.iframe.hidden = true;
      return;
    }
    entry.fail.hidden = true;
    entry.iframe.hidden = false;

    // Reload only when the document actually changed. Reloading on every poll
    // would restart animations and scroll positions once a second.
    var want = data.url + '#rev=' + (data.revision || 0);
    if (entry.loaded !== want) {
      entry.loaded = want;
      entry.iframe.src = data.url;
    }
  }

  function flash(entry) {
    entry.el.classList.add('flash');
    window.setTimeout(function () { entry.el.classList.remove('flash'); }, 900);
  }

  // ---------------------------------------------------------------- state

  function sameShape(a, b) {
    return a.title === b.title && a.status === b.status && a.version === b.version &&
      a.width === b.width && a.height === b.height && a.url === b.url &&
      a.revision === b.revision && a.note === b.note;
  }

  function apply(state) {
    var list = state.artboards || [];
    layout(list);

    var seen = new Set();
    var changedID = null;
    var added = 0;

    list.forEach(function (data) {
      seen.add(data.id);
      var entry = boards.get(data.id);
      if (!entry) {
        entry = buildBoard(data);
        boards.set(data.id, entry);
        paintBoard(entry);
        log(data.title || data.slug || data.id, 'added');
        changedID = data.id;
        added++;
        return;
      }
      var before = entry.data;
      entry.data = data;
      if (!sameShape(before, data)) {
        paintBoard(entry);
        if (before.revision !== data.revision) {
          flash(entry);
          log(data.title || data.slug || data.id, 'updated');
          changedID = data.id;
        } else if (before.status !== data.status) {
          log(data.title || data.slug || data.id, data.status || 'ready');
          changedID = data.id;
        }
      } else {
        // Position can move without any visible property changing, when a
        // board ahead of this one in the row appeared or grew.
        paintBoard(entry);
      }
    });

    boards.forEach(function (entry, id) {
      if (seen.has(id)) return;
      entry.el.remove();
      boards.delete(id);
      log(entry.data.title || id, 'removed');
    });

    empty.hidden = boards.size > 0;
    refreshJump(list);

    if (boards.size === 0) return;
    if (!userPlacedView) {
      fitAll();
    } else if (follow && changedID) {
      focusBoard(changedID, 60);
    } else if (added > 0 && !follow) {
      // A new board may have landed outside the current view; say so rather
      // than moving a viewport the user placed deliberately.
      log('canvas', added + ' new artboard' + (added === 1 ? '' : 's') + ' — press F to fit');
    }
  }

  function refreshJump(list) {
    var current = jump.value;
    jump.textContent = '';
    var head = document.createElement('option');
    head.value = '';
    head.textContent = list.length ? 'Artboards (' + list.length + ')…' : 'Artboards…';
    jump.appendChild(head);
    list.forEach(function (data) {
      var opt = document.createElement('option');
      opt.value = data.id;
      opt.textContent = data.title || data.slug || data.id;
      jump.appendChild(opt);
    });
    if (list.some(function (d) { return d.id === current; })) jump.value = current;
  }

  function log(subject, what) {
    var li = document.createElement('li');
    var time = document.createElement('time');
    var now = new Date();
    time.textContent = String(now.getHours()).padStart(2, '0') + ':' +
      String(now.getMinutes()).padStart(2, '0') + ':' +
      String(now.getSeconds()).padStart(2, '0');
    var text = document.createElement('span');
    text.textContent = subject + ' — ' + what;
    li.appendChild(time);
    li.appendChild(text);
    activityList.insertBefore(li, activityList.firstChild);
    while (activityList.childElementCount > 40) {
      activityList.removeChild(activityList.lastElementChild);
    }
  }

  function setLive(kind, text) {
    dot.className = kind;
    liveText.textContent = text;
  }

  function poll() {
    fetch('artboards', { cache: 'no-store' })
      .then(function (response) {
        if (!response.ok) throw new Error('HTTP ' + response.status);
        return response.json();
      })
      .then(function (state) {
        failures = 0;
        setLive('', state.error ? 'live · ' + state.error : 'live');
        apply(state);
      })
      .catch(function (err) {
        failures++;
        // One missed poll is a hiccup; a run of them means the session behind
        // this canvas is gone, and the user should know why nothing moves.
        if (failures === 3) log('canvas', 'lost contact with Pando (' + err.message + ')');
        setLive(failures >= 3 ? 'down' : 'stale', failures >= 3 ? 'disconnected' : 'reconnecting…');
      });
  }

  // ------------------------------------------------------------ interaction

  function zoomAt(clientX, clientY, factor) {
    var rect = viewport.getBoundingClientRect();
    var px = clientX - rect.left;
    var py = clientY - rect.top;
    var next = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, view.k * factor));
    if (next === view.k) return;
    // Keep the world point under the cursor pinned to the cursor.
    view.x = px - (px - view.x) * (next / view.k);
    view.y = py - (py - view.y) * (next / view.k);
    view.k = next;
    userPlacedView = true;
    applyView();
  }

  viewport.addEventListener('wheel', function (event) {
    event.preventDefault();
    if (event.ctrlKey || event.metaKey) {
      zoomAt(event.clientX, event.clientY, Math.exp(-event.deltaY / 220));
      return;
    }
    // A plain trackpad scroll pans; a mouse wheel usually reports large deltaY
    // in whole lines, and zooming on it is what people expect from a canvas.
    if (event.deltaMode !== 0 || Math.abs(event.deltaY) > 60) {
      zoomAt(event.clientX, event.clientY, Math.exp(-event.deltaY / 500));
      return;
    }
    view.x -= event.deltaX;
    view.y -= event.deltaY;
    userPlacedView = true;
    applyView();
  }, { passive: false });

  var dragging = null;
  viewport.addEventListener('pointerdown', function (event) {
    if (event.button !== 0 && event.button !== 1) return;
    dragging = { x: event.clientX, y: event.clientY, vx: view.x, vy: view.y, id: event.pointerId };
    viewport.setPointerCapture(event.pointerId);
    viewport.classList.add('panning');
  });
  viewport.addEventListener('pointermove', function (event) {
    if (!dragging || dragging.id !== event.pointerId) return;
    view.x = dragging.vx + (event.clientX - dragging.x);
    view.y = dragging.vy + (event.clientY - dragging.y);
    userPlacedView = true;
    applyView();
  });
  function endDrag(event) {
    if (!dragging || dragging.id !== event.pointerId) return;
    viewport.releasePointerCapture(event.pointerId);
    viewport.classList.remove('panning');
    dragging = null;
  }
  viewport.addEventListener('pointerup', endDrag);
  viewport.addEventListener('pointercancel', endDrag);

  document.addEventListener('keydown', function (event) {
    if (event.target && /^(INPUT|SELECT|TEXTAREA)$/.test(event.target.tagName)) return;
    var step = event.shiftKey ? 240 : 80;
    switch (event.key) {
      case '+': case '=': zoomAt(window.innerWidth / 2, window.innerHeight / 2, 1.2); break;
      case '-': case '_': zoomAt(window.innerWidth / 2, window.innerHeight / 2, 1 / 1.2); break;
      case '0': view.k = 1; userPlacedView = true; applyView(); break;
      case 'f': case 'F': userPlacedView = false; fitAll(); break;
      case 'ArrowLeft': view.x += step; userPlacedView = true; applyView(); break;
      case 'ArrowRight': view.x -= step; userPlacedView = true; applyView(); break;
      case 'ArrowUp': view.y += step; userPlacedView = true; applyView(); break;
      case 'ArrowDown': view.y -= step; userPlacedView = true; applyView(); break;
      default: return;
    }
    event.preventDefault();
  });

  document.getElementById('zoom-in').addEventListener('click', function () {
    zoomAt(window.innerWidth / 2, window.innerHeight / 2, 1.2);
  });
  document.getElementById('zoom-out').addEventListener('click', function () {
    zoomAt(window.innerWidth / 2, window.innerHeight / 2, 1 / 1.2);
  });
  document.getElementById('fit').addEventListener('click', function () {
    userPlacedView = false;
    fitAll();
  });
  document.getElementById('actual').addEventListener('click', function () {
    view.k = 1;
    userPlacedView = true;
    applyView();
  });
  followBtn.addEventListener('click', function () {
    follow = !follow;
    followBtn.setAttribute('aria-pressed', String(follow));
  });
  jump.addEventListener('change', function () {
    if (!jump.value) return;
    userPlacedView = true;
    focusBoard(jump.value, 40);
  });
  window.addEventListener('resize', function () {
    if (!userPlacedView) fitAll();
  });

  applyView();
  poll();
  window.setInterval(poll, POLL_MS);
})();
