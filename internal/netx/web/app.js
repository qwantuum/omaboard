(() => {
  const canvas = document.getElementById('board');
  const ctx = canvas.getContext('2d');
  const connDot = document.getElementById('conn');
  const connText = document.getElementById('connText');
  const countEl = document.getElementById('count');
  const toolNameEl = document.getElementById('toolName');

  const TOOL_NAMES = {
    select: 'Выбор', hand: 'Рука', pen: 'Перо', line: 'Линия', rect: 'Прямоугольник',
    ellipse: 'Эллипс', text: 'Текст', eraser: 'Ластик',
  };

  let palette = null;
  let elements = [];       // always an array
  let tool = 'select';
  let color = '#d8dee9';
  let strokeW = 3;
  let drawing = false;
  let current = null;
  let selected = null;     // element id
  let drag = null;         // { orig, el, ox, oy, moved }
  let view = { x: 0, y: 0 };   // world coords of the canvas top-left corner
  const touches = new Map();   // active touch pointers
  let pan = null;              // { sx, sy, vx, vy } hand / middle / right pan
  let touchPan = null;         // two-finger pan
  const undoStack = [];
  const redoStack = [];

  // ---------- palette ----------
  function applyPalette(p) {
    if (!p) return;
    palette = p;
    const root = document.documentElement.style;
    root.setProperty('--window', p.window);
    root.setProperty('--surface', p.surface);
    root.setProperty('--canvas', p.canvas);
    root.setProperty('--fg', p.fg);
    root.setProperty('--muted', p.muted);
    root.setProperty('--accent', p.accent);
    root.setProperty('--border', p.border);
    root.setProperty('--hover', p.hover);
    root.setProperty('--on-accent', onAccent(p.accent));
    if (!userPickedColor) {
      color = p.fg;
      document.getElementById('color').value = p.fg;
    }
    render();
  }

  function onAccent(hex) {
    const { r, g, b } = parseHex(hex);
    return (0.2126 * r + 0.7152 * g + 0.0722 * b) > 140 ? '#000000' : '#ffffff';
  }
  function parseHex(c) {
    c = c.replace('#', '');
    if (c.length === 3) c = c.split('').map(x => x + x).join('');
    return {
      r: parseInt(c.slice(0, 2), 16),
      g: parseInt(c.slice(2, 4), 16),
      b: parseInt(c.slice(4, 6), 16),
    };
  }
  function hexRGBA(c, a = 1) {
    const { r, g, b } = parseHex(c);
    return `rgba(${r},${g},${b},${a})`;
  }

  // ---------- ops ----------
  function applyOps(ops) {
    for (const op of ops) {
      if (op.kind === 'add') {
        const i = elements.findIndex(e => e.id === op.el.id);
        if (i >= 0) elements[i] = op.el; else elements.push(op.el);
      } else if (op.kind === 'del') {
        elements = elements.filter(e => e.id !== op.el.id);
        if (selected === op.el.id) selected = null;
      } else if (op.kind === 'upd') {
        const i = elements.findIndex(e => e.id === op.el.id);
        if (i >= 0) elements[i] = op.el; else elements.push(op.el);
      }
    }
    updateCount();
    render();
  }

  function commit(ops) {
    undoStack.push(ops);
    if (undoStack.length > 100) undoStack.shift();
    redoStack.length = 0;
    applyOps(ops);
    send({ type: 'ops', ops });
  }

  function invert(ops) {
    return ops.map(op => {
      if (op.kind === 'add') return { kind: 'del', el: op.el };
      if (op.kind === 'del') return { kind: 'add', el: op.el };
      if (op.kind === 'upd' && op.prev) return { kind: 'upd', el: op.prev, prev: op.el };
      return op;
    });
  }

  function undo() {
    const ops = undoStack.pop();
    if (!ops) return;
    const inv = invert(ops);
    redoStack.push(ops);
    applyOps(inv);
    send({ type: 'ops', ops: inv });
  }
  function redo() {
    const ops = redoStack.pop();
    if (!ops) return;
    undoStack.push(ops);
    applyOps(ops);
    send({ type: 'ops', ops });
  }

  // ---------- rendering ----------
  function resize() {
    const dpr = window.devicePixelRatio || 1;
    canvas.width = canvas.clientWidth * dpr;
    canvas.height = canvas.clientHeight * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    render();
  }

  function render() {
    const w = canvas.clientWidth, h = canvas.clientHeight;
    ctx.clearRect(0, 0, w, h);
    if (palette) {
      ctx.fillStyle = hexRGBA(palette.canvas);
      ctx.fillRect(0, 0, w, h);
      ctx.fillStyle = hexRGBA(palette.border, 0.6);
      const step = 28;
      const x0 = Math.floor(view.x / step) * step;
      const y0 = Math.floor(view.y / step) * step;
      for (let x = x0; x < view.x + w; x += step)
        for (let y = y0; y < view.y + h; y += step)
          ctx.fillRect(x - view.x, y - view.y, 1.5, 1.5);
    }
    ctx.save();
    ctx.translate(-view.x, -view.y);
    for (const el of elements) drawElement(el, ctx);
    if (current) drawElement(current, ctx);
    if (drag) drawSelection(drag.el, ctx);
    else if (selected) {
      const el = elements.find(e => e.id === selected);
      if (el) drawSelection(el, ctx);
    }
    ctx.restore();
  }

  function style(el, c) {
    c.strokeStyle = el.color;
    c.fillStyle = el.color;
    c.lineWidth = el.strokeW > 0 ? el.strokeW : 2;
    c.lineCap = 'round';
    c.lineJoin = 'round';
  }

  function drawElement(el, c = ctx) {
    style(el, c);
    if (el.type === 'pen') {
      if (el.points.length < 2) return;
      c.beginPath();
      el.points.forEach((p, i) => i ? c.lineTo(p.x, p.y) : c.moveTo(p.x, p.y));
      c.stroke();
    } else if (el.type === 'line') {
      c.beginPath();
      c.moveTo(el.points[0].x, el.points[0].y);
      c.lineTo(el.points[1].x, el.points[1].y);
      c.stroke();
    } else if (el.type === 'rect') {
      const [x, y, w, h] = normRect(el);
      c.strokeRect(x, y, w, h);
    } else if (el.type === 'ellipse') {
      const [x, y, w, h] = normRect(el);
      if (Math.abs(w) < 1 || Math.abs(h) < 1) return;
      c.beginPath();
      c.ellipse(x + w / 2, y + h / 2, Math.abs(w / 2), Math.abs(h / 2), 0, 0, Math.PI * 2);
      c.stroke();
    } else if (el.type === 'text') {
      const size = el.strokeW * 6 + 10;
      c.font = `${size}px system-ui, sans-serif`;
      c.fillText(el.text, el.x, el.y);
    }
  }

  function drawSelection(el, c = ctx) {
    const [x, y, w, h] = bounds(el);
    c.save();
    c.strokeStyle = palette ? palette.accent : '#88c0d0';
    c.lineWidth = 1.5;
    c.setLineDash([5, 4]);
    c.strokeRect(x - 4, y - 4, w + 8, h + 8);
    c.restore();
  }

  function normRect(el) {
    let { x, y, w, h } = el;
    if (w < 0) { x += w; w = -w; }
    if (h < 0) { y += h; h = -h; }
    return [x, y, w, h];
  }

  function bounds(el) {
    if (el.type === 'rect' || el.type === 'ellipse') return normRect(el);
    if (el.type === 'text') {
      const size = el.strokeW * 6 + 10;
      return [el.x, el.y - size * 0.8, el.text.length * size * 0.55, size];
    }
    if (!el.points || !el.points.length) return [el.x, el.y, 0, 0];
    let minX = el.points[0].x, minY = el.points[0].y, maxX = minX, maxY = minY;
    for (const p of el.points) {
      minX = Math.min(minX, p.x); maxX = Math.max(maxX, p.x);
      minY = Math.min(minY, p.y); maxY = Math.max(maxY, p.y);
    }
    return [minX, minY, maxX - minX, maxY - minY];
  }

  // ---------- hit testing ----------
  function hitTest(x, y) {
    for (let i = elements.length - 1; i >= 0; i--) {
      if (hits(elements[i], x, y)) return elements[i];
    }
    return null;
  }

  function hits(el, x, y) {
    const tol = Math.max((el.strokeW || 2) / 2 + 3, 5);
    if (el.type === 'rect' || el.type === 'ellipse') {
      const [bx, by, bw, bh] = normRect(el);
      return x >= bx - tol && x <= bx + bw + tol && y >= by - tol && y <= by + bh + tol;
    }
    if (el.type === 'text') {
      const [bx, by, bw, bh] = bounds(el);
      return x >= bx - tol && x <= bx + bw + tol && y >= by - tol && y <= by + bh + tol;
    }
    const pts = el.points;
    if (!pts || !pts.length) return false;
    if (pts.length === 1) return Math.hypot(pts[0].x - x, pts[0].y - y) <= tol;
    for (let i = 0; i < pts.length - 1; i++) {
      if (distToSeg(x, y, pts[i], pts[i + 1]) <= tol) return true;
    }
    return false;
  }

  function distToSeg(px, py, a, b) {
    const dx = b.x - a.x, dy = b.y - a.y;
    const l2 = dx * dx + dy * dy;
    if (!l2) return Math.hypot(px - a.x, py - a.y);
    let t = ((px - a.x) * dx + (py - a.y) * dy) / l2;
    t = Math.max(0, Math.min(1, t));
    return Math.hypot(px - (a.x + t * dx), py - (a.y + t * dy));
  }

  function moveEl(el, dx, dy) {
    if (el.points) el.points = el.points.map(p => ({ x: p.x + dx, y: p.y + dy }));
    el.x += dx; el.y += dy;
  }

  // ---------- pointer ----------
  function pos(e) {
    const r = canvas.getBoundingClientRect();
    return { x: e.clientX - r.left, y: e.clientY - r.top };
  }
  function world(p) {
    return { x: p.x + view.x, y: p.y + view.y };
  }
  function cursorFor(t) {
    return t === 'hand' ? 'grab' : t === 'select' ? 'default' : 'crosshair';
  }
  function startPan(p) {
    pan = { sx: p.x, sy: p.y, vx: view.x, vy: view.y };
    canvas.style.cursor = 'grabbing';
  }
  function centroid() {
    let x = 0, y = 0;
    for (const p of touches.values()) { x += p.x; y += p.y; }
    return { x: x / touches.size, y: y / touches.size };
  }
  const clone = el => JSON.parse(JSON.stringify(el));

  canvas.addEventListener('pointerdown', e => {
    const p = pos(e);

    if (e.pointerType === 'touch') {
      touches.set(e.pointerId, p);
      if (touches.size >= 2) {
        current = null;
        drawing = false;
        drag = null;
        pan = null;
        const c = centroid();
        touchPan = { sx: c.x, sy: c.y, vx: view.x, vy: view.y };
        render();
        return;
      }
    }

    if (e.button === 1 || e.button === 2) {
      canvas.setPointerCapture(e.pointerId);
      startPan(p);
      return;
    }
    if (e.button !== 0) return;
    canvas.setPointerCapture(e.pointerId);

    if (tool === 'hand') {
      startPan(p);
      return;
    }

    const w = world(p);
    if (tool === 'select') {
      const el = hitTest(w.x, w.y);
      selected = el ? el.id : null;
      if (el) drag = { orig: clone(el), el: clone(el), sx: w.x, sy: w.y, moved: false };
      render();
      return;
    }
    if (tool === 'eraser') {
      const el = hitTest(w.x, w.y);
      if (el) commit([{ kind: 'del', el }]);
      return;
    }
    if (tool === 'text') {
      const text = prompt('Текст:');
      if (text) {
        commit([{
          kind: 'add',
          el: { id: uuid(), type: 'text', color, strokeW, x: w.x, y: w.y, w: 0, h: 0, text },
        }]);
      }
      return;
    }
    drawing = true;
    current = {
      id: uuid(), type: tool, color, strokeW,
      points: [w, { ...w }], x: w.x, y: w.y, w: 0, h: 0, text: '',
    };
  });

  canvas.addEventListener('pointermove', e => {
    const p = pos(e);
    if (e.pointerType === 'touch' && touches.has(e.pointerId)) {
      touches.set(e.pointerId, p);
    }
    if (touchPan) {
      const c = centroid();
      view.x = touchPan.vx - (c.x - touchPan.sx);
      view.y = touchPan.vy - (c.y - touchPan.sy);
      render();
      return;
    }
    if (pan) {
      view.x = pan.vx - (p.x - pan.sx);
      view.y = pan.vy - (p.y - pan.sy);
      render();
      return;
    }
    if (!drag && (!drawing || !current)) return;
    const w = world(p);
    if (drag) {
      const dx = w.x - drag.sx, dy = w.y - drag.sy;
      if (dx || dy) {
        drag.moved = true;
        drag.el = clone(drag.orig);
        moveEl(drag.el, dx, dy);
        render();
      }
      return;
    }
    if (current.type === 'pen') {
      current.points.push(w);
    } else if (current.type === 'line') {
      current.points[1] = w;
    } else {
      current.w = w.x - current.x;
      current.h = w.y - current.y;
    }
    render();
  });

  function finishPointer(e) {
    if (e && e.pointerType === 'touch') {
      touches.delete(e.pointerId);
      if (touchPan) {
        if (touches.size) {
          const c = centroid();
          touchPan = { sx: c.x, sy: c.y, vx: view.x, vy: view.y };
        } else {
          touchPan = null;
        }
      }
    }
    if (pan) {
      pan = null;
      canvas.style.cursor = cursorFor(tool);
      return;
    }
    if (drag) {
      if (drag.moved) {
        commit([{ kind: 'upd', el: drag.el, prev: drag.orig }]);
        selected = drag.el.id;
      }
      drag = null;
      render();
      return;
    }
    if (!drawing || !current) return;
    drawing = false;
    const el = current;
    current = null;
    if (el.type === 'pen') {
      if (el.points.length < 3) { render(); return; }
      el.points = simplify(el.points);
    } else {
      if (Math.abs(el.w) < 2 && Math.abs(el.h) < 2) { render(); return; }
      if (el.type === 'line') {
        el.points = [el.points[0], { x: el.x + el.w, y: el.y + el.h }];
      } else {
        const [x, y, w, h] = normRect(el);
        el.x = x; el.y = y; el.w = w; el.h = h;
      }
    }
    commit([{ kind: 'add', el }]);
  }
  canvas.addEventListener('pointerup', finishPointer);
  canvas.addEventListener('pointercancel', finishPointer);

  canvas.addEventListener('wheel', e => {
    e.preventDefault();
    if (drawing || drag) return;
    view.x += e.deltaX;
    view.y += e.deltaY;
    render();
  }, { passive: false });

  canvas.addEventListener('contextmenu', e => e.preventDefault());

  // light point decimation for freehand strokes
  function simplify(pts) {
    const out = [pts[0]];
    for (let i = 1; i < pts.length - 1; i++) {
      const last = out[out.length - 1];
      if (Math.hypot(pts[i].x - last.x, pts[i].y - last.y) >= 1.5) out.push(pts[i]);
    }
    out.push(pts[pts.length - 1]);
    return out;
  }

  function uuid() {
    return crypto.randomUUID ? crypto.randomUUID() :
      'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, c => {
        const r = Math.random() * 16 | 0;
        return (c === 'x' ? r : (r & 3 | 8)).toString(16);
      });
  }

  // ---------- toolbar ----------
  function setTool(t) {
    tool = t;
    selected = null;
    document.querySelectorAll('#tools button').forEach(b =>
      b.classList.toggle('active', b.dataset.tool === t));
    toolNameEl.textContent = TOOL_NAMES[t] || t;
    canvas.style.cursor = cursorFor(t);
    render();
  }
  document.querySelectorAll('#tools button').forEach(b =>
    b.addEventListener('click', () => setTool(b.dataset.tool)));

  let userPickedColor = false;
  document.getElementById('color').addEventListener('input', e => {
    color = e.target.value; userPickedColor = true;
  });
  document.getElementById('strokeW').addEventListener('input', e => strokeW = +e.target.value);
  document.getElementById('undo').addEventListener('click', undo);
  document.getElementById('redo').addEventListener('click', redo);
  document.getElementById('clear').addEventListener('click', () => {
    if (!elements.length) return;
    commit(elements.map(el => ({ kind: 'del', el })));
    selected = null;
  });
  document.getElementById('exportPng').addEventListener('click', () => {
    const pad = 40;
    let x0, y0, x1, y1;
    if (!elements.length) {
      x0 = view.x; y0 = view.y;
      x1 = x0 + canvas.clientWidth; y1 = y0 + canvas.clientHeight;
    } else {
      x0 = Infinity; y0 = Infinity; x1 = -Infinity; y1 = -Infinity;
      for (const el of elements) {
        const [bx, by, bw, bh] = bounds(el);
        x0 = Math.min(x0, bx); y0 = Math.min(y0, by);
        x1 = Math.max(x1, bx + bw); y1 = Math.max(y1, by + bh);
      }
      x0 -= pad; y0 -= pad; x1 += pad; y1 += pad;
    }
    const w = Math.max(1, Math.round(x1 - x0));
    const h = Math.max(1, Math.round(y1 - y0));
    const off = document.createElement('canvas');
    off.width = w; off.height = h;
    const o = off.getContext('2d');
    if (palette) {
      o.fillStyle = hexRGBA(palette.canvas);
      o.fillRect(0, 0, w, h);
    }
    o.save();
    o.translate(-x0, -y0);
    for (const el of elements) drawElement(el, o);
    o.restore();
    const a = document.createElement('a');
    a.download = `omaboard-${Date.now()}.png`;
    a.href = off.toDataURL('image/png');
    a.click();
  });

  const keymap = {
    v: 'select', h: 'hand', p: 'pen', l: 'line', r: 'rect',
    o: 'ellipse', t: 'text', e: 'eraser',
  };
  addEventListener('keydown', e => {
    if (e.target.tagName === 'INPUT') return;
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'z') {
      e.preventDefault(); e.shiftKey ? redo() : undo(); return;
    }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'y') {
      e.preventDefault(); redo(); return;
    }
    const k = e.key.toLowerCase();
    if (keymap[k]) setTool(keymap[k]);
  });

  function updateCount() {
    const n = elements.length;
    const forms = ['объект', 'объекта', 'объектов'];
    const n10 = n % 10, n100 = n % 100;
    const word = n10 === 1 && n100 !== 11 ? forms[0] :
      n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14) ? forms[1] : forms[2];
    countEl.textContent = `${n} ${word}`;
  }

  // ---------- websocket ----------
  let ws, retry = 0;
  function send(msg) {
    if (ws && ws.readyState === 1) ws.send(JSON.stringify(msg));
  }
  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${location.host}/ws`);
    ws.onopen = () => {
      retry = 0;
      connDot.classList.add('ok');
      connText.textContent = 'синхронизировано';
    };
    ws.onmessage = e => {
      const msg = JSON.parse(e.data);
      if (msg.type === 'sync') {
        elements = msg.elements || [];
        if (msg.palette) applyPalette(msg.palette);
        selected = null;
        updateCount(); render();
      } else if (msg.type === 'ops') {
        applyOps(msg.ops || []);
      } else if (msg.type === 'theme') {
        applyPalette(msg.palette);
      }
    };
    ws.onclose = () => {
      connDot.classList.remove('ok');
      connText.textContent = 'переподключение…';
      setTimeout(connect, Math.min(1000 * 2 ** retry++, 8000));
    };
    ws.onerror = () => ws.close();
  }

  addEventListener('resize', resize);
  resize();
  updateCount();
  connect();
})();
