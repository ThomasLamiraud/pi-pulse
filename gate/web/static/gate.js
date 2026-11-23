"use strict"

// Home gate: shows a card per app, refreshed from /api/status every few
// seconds.

const REFRESH_MS = 10_000
const ACCENTS = ["coral", "violet", "cyan", "lime", "amber"]
const DASH = "—"

function isNumber(value) {
  return typeof value === "number" && Number.isFinite(value)
}

function uptime(seconds) {
  if (!isNumber(seconds)) return ""
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  if (days > 0) return `up ${days} ${days === 1 ? "day" : "days"}`
  if (hours > 0) return `up ${hours} ${hours === 1 ? "hour" : "hours"}`
  return `up ${Math.max(1, Math.floor(seconds / 60))} min`
}

// Links use the host the page was opened with (homemonitor.local or an IP),
// unless the app runs on another machine.
function appLink(app) {
  const host = app.host || window.location.hostname
  const port = app.port === 80 ? "" : `:${app.port}`
  return `http://${host}${port}${app.path || "/"}`
}

function appAddress(app) {
  return app.host ? `${app.host}:${app.port}` : `:${app.port}`
}

function statusLabel(status) {
  return { up: "Up", down: "Down" }[status] || "Checking"
}

function renderApp(app, index) {
  const card = document.createElement("a")
  card.className = `app app--${ACCENTS[index % ACCENTS.length]} app--${app.status}`
  card.href = appLink(app)

  const icon = document.createElement("span")
  icon.className = "app-icon"
  icon.setAttribute("aria-hidden", "true")
  const glyph = document.createElement("i")
  glyph.className = `ti ti-${app.icon}`
  icon.append(glyph)

  const copy = document.createElement("span")
  copy.className = "app-copy"
  const name = document.createElement("strong")
  name.textContent = app.name
  const address = document.createElement("small")
  address.textContent = appAddress(app)
  copy.append(name, address)

  const dot = document.createElement("span")
  dot.className = "app-status"
  dot.title = statusLabel(app.status)
  dot.setAttribute("role", "img")
  dot.setAttribute("aria-label", statusLabel(app.status))

  card.append(icon, copy, dot)
  return card
}

function renderOverall(apps) {
  const overall = document.getElementById("overall")
  const label = overall.querySelector("span")
  const down = apps.filter((app) => app.status === "down").length
  const checking = apps.some((app) => app.status === "unknown")

  if (apps.length === 0) {
    overall.className = "status status--unknown"
    label.textContent = "No apps configured"
  } else if (checking) {
    overall.className = "status status--unknown"
    label.textContent = "Checking apps…"
  } else if (down > 0) {
    overall.className = "status status--down"
    label.textContent = `${down} ${down === 1 ? "app" : "apps"} down`
  } else {
    overall.className = "status status--up"
    label.textContent = "All apps up"
  }
}

function render(status) {
  document.title = status.title || "Home"
  document.getElementById("title").textContent = status.title || "Home"
  document.getElementById("host-details").textContent =
    [status.model || "Raspberry Pi", uptime(status.uptime_seconds)].filter(Boolean).join(" · ")

  const apps = status.apps || []
  const grid = document.getElementById("apps")
  if (apps.length === 0) {
    const empty = document.createElement("p")
    empty.className = "empty"
    empty.textContent = "Add apps to the gate's config.yaml to list them here."
    grid.replaceChildren(empty)
  } else {
    grid.replaceChildren(...apps.map(renderApp))
  }
  renderOverall(apps)

  document.getElementById("footer").textContent = status.checked_at
    ? `Checked at ${new Date(status.checked_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}`
    : "Checking…"
}

async function refresh() {
  try {
    const response = await fetch("/api/status", { cache: "no-store" })
    if (!response.ok) throw new Error(`Status request failed: ${response.status}`)
    render(await response.json())
  } catch (error) {
    console.error(error)
    document.getElementById("footer").textContent = "Can't reach the gate. Retrying…"
  }
}

document.addEventListener("DOMContentLoaded", () => {
  refresh()
  window.setInterval(refresh, REFRESH_MS)
})
