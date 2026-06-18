// REALWORLD_JS_UTIL formats display labels for the UI.

export function formatLabel(value) {
  if (value == null || value === "") {
    return "—";
  }
  return String(value).trim();
}

export function debounce(fn, ms) {
  let timer;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}
