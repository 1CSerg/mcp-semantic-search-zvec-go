export function formatLabel(name) {
  return String(name).trim().toLowerCase();
}

export function mergeDefaults(target, defaults) {
  return { ...defaults, ...target };
}
