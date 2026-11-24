export function registerServiceWorker(): void {
  if (typeof navigator === "undefined" || !("serviceWorker" in navigator)) return;
  function register(): void {
    navigator.serviceWorker.register("/sw.js").catch(() => {
      // offline shell unavailable; app still works online
    });
  }
  if (document.readyState === "complete") {
    register();
  } else {
    window.addEventListener("load", register, { once: true });
  }
}
