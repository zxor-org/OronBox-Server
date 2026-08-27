const storageKey = "oronbox_server_theme"

export type Theme = "dark" | "light"

export function readTheme(): Theme {
  return localStorage.getItem(storageKey) === "light" ? "light" : "dark"
}

export function applyTheme(theme: Theme) {
  document.documentElement.classList.remove("dark-theme", "light-theme")
  document.documentElement.classList.add(`${theme}-theme`)
  document.documentElement.style.colorScheme = theme
  localStorage.setItem(storageKey, theme)
}

export function initTheme() {
  applyTheme(readTheme())
}
