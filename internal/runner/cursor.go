package runner

// cursorScript injects a fake mouse cursor into every page so that pointer
// movement and clicks are visible in the recorded video. Playwright's real
// mouse actions dispatch native mousemove/mousedown events, which this overlay
// listens for. Added via AddInitScript so it survives navigations.
const cursorScript = `
(() => {
  if (window.__vhswebCursorInstalled) return;
  window.__vhswebCursorInstalled = true;

  const install = () => {
    if (!document.body) { requestAnimationFrame(install); return; }

    const cursor = document.createElement('div');
    cursor.setAttribute('data-vhsweb-cursor', '');
    const cx = window.innerWidth / 2, cy = window.innerHeight / 2;
    Object.assign(cursor.style, {
      position: 'fixed',
      top: '0', left: '0',
      width: '32px', height: '32px',
      marginLeft: '-3px', marginTop: '-3px',
      pointerEvents: 'none',
      zIndex: '2147483647',
      transition: 'transform 0.05s linear',
      background: 'transparent',
      // Start at the viewport center so a freshly loaded page (including one
      // reached by clicking a link) never shows the cursor parked top-left.
      transform: 'translate(' + cx + 'px, ' + cy + 'px)',
    });
    // Size via inline style, not width/height attributes — a page CSS rule for
    // svg elements would otherwise override the attributes.
    cursor.innerHTML =
      '<svg viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg" ' +
      'style="display:block;width:32px;height:32px">' +
      '<path d="M2 2 L2 16 L6 12 L9 18 L11 17 L8 11 L13 11 Z" ' +
      'fill="black" stroke="white" stroke-width="1.2"/></svg>';
    document.body.appendChild(cursor);

    // Track the cursor position on window so the recorder can resume an
    // animation from wherever the cursor actually is — e.g. the center after a
    // navigation recreated this overlay.
    const setPos = (x, y) => {
      window.__vhswebCursorX = x;
      window.__vhswebCursorY = y;
      cursor.style.transform = 'translate(' + x + 'px, ' + y + 'px)';
    };
    setPos(cx, cy);
    document.addEventListener('mousemove', (e) => setPos(e.clientX, e.clientY), true);

    const ripple = (e) => {
      const r = document.createElement('div');
      Object.assign(r.style, {
        position: 'fixed',
        left: e.clientX + 'px', top: e.clientY + 'px',
        width: '8px', height: '8px',
        marginLeft: '-4px', marginTop: '-4px',
        borderRadius: '50%',
        border: '2px solid rgba(0,0,0,0.6)',
        pointerEvents: 'none',
        zIndex: '2147483646',
        transform: 'scale(1)',
        opacity: '1',
        transition: 'transform 0.4s ease-out, opacity 0.4s ease-out',
      });
      document.body.appendChild(r);
      requestAnimationFrame(() => {
        r.style.transform = 'scale(4)';
        r.style.opacity = '0';
      });
      setTimeout(() => r.remove(), 450);
    };
    document.addEventListener('mousedown', ripple, true);
  };
  install();
})();
`
