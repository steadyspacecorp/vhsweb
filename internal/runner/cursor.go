package runner

import "fmt"

// zoomScript magnifies the whole page by factor so that a viewport sized to the
// target output resolution shows content as if it were a smaller logical
// viewport scaled up. Applied via CSS zoom on the root element, kept in place
// across DOM changes. Injected as an init script so it survives navigations.
func zoomScript(factor float64) string {
	return fmt.Sprintf(`
(() => {
  if (window.__vhswebZoomInstalled) return;
  window.__vhswebZoomInstalled = true;
  const apply = () => {
    if (!document.documentElement) { requestAnimationFrame(apply); return; }
    document.documentElement.style.zoom = '%g';
  };
  apply();
  document.addEventListener('DOMContentLoaded', apply);
})();
`, factor)
}

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
    Object.assign(cursor.style, {
      position: 'fixed',
      top: '0', left: '0',
      width: '32px', height: '32px',
      marginLeft: '-3px', marginTop: '-3px',
      pointerEvents: 'none',
      zIndex: '2147483647',
      transition: 'transform 0.05s linear',
      background: 'transparent',
    });
    cursor.innerHTML =
      '<svg width="32" height="32" viewBox="0 0 20 20" fill="none" ' +
      'xmlns="http://www.w3.org/2000/svg">' +
      '<path d="M2 2 L2 16 L6 12 L9 18 L11 17 L8 11 L13 11 Z" ' +
      'fill="black" stroke="white" stroke-width="1.2"/></svg>';
    document.body.appendChild(cursor);

    document.addEventListener('mousemove', (e) => {
      cursor.style.transform =
        'translate(' + e.clientX + 'px, ' + e.clientY + 'px)';
    }, true);

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
