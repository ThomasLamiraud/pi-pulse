"use strict"

// PiPulse dashboard: loads the latest sample and 24h history from
// /api/dashboard, then follows new samples on the /events stream.

const MAX_CHART_POINTS = 720
const KEEP_RECENT_POINTS = 120

const state = {
  charts: [],
  latest: null,
  freshnessThreshold: 45,
  stream: null,
  reloadOnOpen: false
}

// ---- Formatting (mirrors the Rails view helpers) -------------------------

const DASH = "—"

function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (char) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"
  })[char])
}

function isNumber(value) {
  return typeof value === "number" && Number.isFinite(value)
}

function formatNumber(value, precision = 2) {
  return isNumber(value) ? String(Number(value.toFixed(precision))) : DASH
}

function formatPercentage(value) {
  return isNumber(value) ? `${formatNumber(value, 1)}%` : DASH
}

function formatBytes(value) {
  if (!isNumber(value)) return DASH
  if (value < 1024) return `${Math.round(value)} ${Math.round(value) === 1 ? "Byte" : "Bytes"}`

  const units = ["KB", "MB", "GB", "TB", "PB"]
  let exponent = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length)
  let amount = Number((value / 1024 ** exponent).toPrecision(3))
  if (amount >= 1024 && exponent < units.length) {
    exponent += 1
    amount = Number((value / 1024 ** exponent).toPrecision(3))
  }
  return `${amount} ${units[exponent - 1]}`
}

function formatRate(value) {
  return isNumber(value) ? `${formatBytes(value)}/s` : DASH
}

function formatTemperature(value) {
  return isNumber(value) ? `${formatNumber(value, 1)}°C` : DASH
}

function formatFrequency(value) {
  return isNumber(value) ? `${formatNumber(value / 1_000_000, 0)} MHz` : DASH
}

function formatCount(value) {
  return isNumber(value) ? value.toLocaleString() : DASH
}

function timeAgo(timestamp) {
  if (!timestamp) return DASH
  const seconds = Math.max(0, (Date.now() - Date.parse(timestamp)) / 1000)
  return `${ageLabel(seconds)} ago`
}

function pluralize(count, word) {
  return `${count} ${count === 1 ? word : `${word}s`}`
}

function ageLabel(seconds) {
  if (seconds < 60) return `${Math.floor(seconds)} seconds`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} minutes`
  return `${Math.floor(seconds / 3600)} hours`
}

// ---- Status helpers --------------------------------------------------------

const UNDER_VOLTAGE = 1 << 0
const FREQUENCY_CAPPED = 1 << 1
const THROTTLED = 1 << 2
const SOFT_TEMPERATURE_LIMIT = 1 << 3
const HISTORICAL_THROTTLING_MASK = 0xF0000

function hardwareHealth(sample) {
  const flags = sample?.throttling_flags
  if (!isNumber(flags)) return ["Hardware status unavailable", "unknown"]
  if (flags & UNDER_VOLTAGE) return ["Undervoltage detected", "warning"]
  if (flags & THROTTLED) return ["CPU is throttled", "warning"]
  if (flags & SOFT_TEMPERATURE_LIMIT) return ["Temperature limit active", "warning"]
  if (flags & FREQUENCY_CAPPED) return ["CPU frequency is capped", "warning"]
  if (flags & HISTORICAL_THROTTLING_MASK) return ["Past power or thermal issue", "history"]
  return ["Power and clocks stable", "stable"]
}

function sampleAge(sample) {
  return Math.max(0, (Date.now() - Date.parse(sample.sampled_at)) / 1000)
}

function collectorState(sample) {
  if (!sample) return ["Waiting for collector", "waiting"]
  if (sample.status === "error") return ["Collector error", "error"]
  if (sampleAge(sample) >= state.freshnessThreshold) return ["Data is stale", "stale"]
  return ["Live", "live"]
}

// ---- Metrics section -------------------------------------------------------

function meter(value) {
  return `<div class="meter"><i style="--value: ${isNumber(value) ? value : 0}%"></i></div>`
}

function renderMetrics(sample) {
  const [stateLabel, stateClass] = collectorState(sample)
  const [hardwareLabel, hardwareClass] = hardwareHealth(sample)
  const s = sample || {}
  const filesystems = s.filesystems || []
  const interfaces = Object.entries(s.network_interfaces || {}).sort(([a], [b]) => a.localeCompare(b))
  const topCpu = s.top_cpu_processes || []
  const topMemory = s.top_memory_processes || []
  const databases = s.databases || []

  const sampleTime = sample
    ? `<span class="sample-time" data-sample-time>Last sample ${ageLabel(sampleAge(sample))} ago</span>`
    : `<span class="sample-time">Start <code>pipulse</code> to begin sampling.</span>`

  const alert = s.status === "error"
    ? `<div class="collector-alert" role="alert"><strong>The last collection failed.</strong><span>${escapeHtml(s.error_message)}</span></div>`
    : ""

  const cpuFrequency = isNumber(s.cpu_max_frequency_hz)
    ? `${formatFrequency(s.cpu_frequency_hz)} / ${formatFrequency(s.cpu_max_frequency_hz)} max`
    : formatFrequency(s.cpu_frequency_hz)

  const swap = s.swap_total_bytes > 0
    ? `<small>Swap ${formatBytes(s.swap_used_bytes)} / ${formatBytes(s.swap_total_bytes)} · ${formatPercentage(s.swap_usage)}</small>`
    : ""

  const disks = filesystems.length
    ? filesystems.map((filesystem) => `
        <div class="disk">
          <div class="disk-copy">
            <strong>${escapeHtml(filesystem.mountpoint)}</strong>
            <span>${escapeHtml(filesystem.source)} · ${escapeHtml(filesystem.type)}</span>
          </div>
          <div class="disk-usage">
            <span>${formatPercentage(filesystem.usage)}</span>
            <small>${formatBytes(filesystem.used_bytes)} / ${formatBytes(filesystem.total_bytes)}</small>
          </div>
          ${meter(filesystem.usage)}
        </div>`).join("")
    : `<p class="empty-state">Filesystem data will appear after the first successful sample.</p>`

  const interfaceRows = interfaces.length
    ? interfaces.map(([name, rates]) => `
        <div class="interface">
          <strong>${escapeHtml(name)}</strong>
          <span>↓ ${formatRate(rates.received_bytes_per_second)}</span>
          <span>↑ ${formatRate(rates.transmitted_bytes_per_second)}</span>
        </div>`).join("")
    : `<p class="empty-state">No active physical network interface was found.</p>`

  const tableList = (database) => {
    if (database.error) return `<p class="database-note database-note--error">${escapeHtml(database.error)}</p>`
    if (!database.tables) return `<p class="database-note">Counting rows…</p>`
    if (!database.tables.length) return `<p class="database-note">No tables</p>`
    return `<div class="database-tables">${database.tables.map((table) => `
      <span><b>${escapeHtml(table.name)}</b> ${formatCount(table.rows)}</span>`).join("")}</div>`
  }

  const databaseRows = databases.length
    ? databases.map((database) => `
        <div class="database">
          <div class="disk-copy">
            <strong>${escapeHtml(database.name)}</strong>
            <span>${escapeHtml(database.path)}</span>
          </div>
          <div class="disk-usage">
            <span>${formatBytes(database.size_bytes)}</span>
            <small>${database.modified_at ? `written ${timeAgo(database.modified_at)}` : ""}</small>
          </div>
          ${tableList(database)}
        </div>`).join("")
    : `<p class="empty-state">Database data will appear after the first successful sample.</p>`

  const processRows = (processes, value, emptyMessage) => processes.length
    ? processes.map((process) => `
        <div class="process">
          <span class="process-name">${escapeHtml(process.name)}</span>
          <small>PID ${escapeHtml(process.pid)}</small>
          <strong>${value(process)}</strong>
        </div>`).join("")
    : `<p class="empty-state">${emptyMessage}</p>`

  document.getElementById("dashboard_metrics").innerHTML = `
    <div class="status-row">
      <span class="status status--${stateClass}" data-status>
        <i aria-hidden="true"></i>
        <span data-status-label>${stateLabel}</span>
      </span>
      ${sampleTime}
    </div>

    ${alert}

    <div class="metric-grid">
      <article class="metric-card metric-card--coral">
        <div class="metric-heading"><span>CPU</span><span class="metric-icon" aria-hidden="true">⌁</span></div>
        <strong>${formatPercentage(s.cpu_usage)}</strong>
        ${meter(s.cpu_usage)}
        <small>${cpuFrequency}</small>
      </article>

      <article class="metric-card metric-card--violet">
        <div class="metric-heading"><span>Memory</span><span class="metric-icon" aria-hidden="true">◫</span></div>
        <strong>${formatPercentage(s.memory_usage)}</strong>
        ${meter(s.memory_usage)}
        <div class="memory-details">
          <small>RAM ${formatBytes(s.memory_used_bytes)} / ${formatBytes(s.memory_total_bytes)}</small>
          ${swap}
        </div>
      </article>

      <article class="metric-card metric-card--amber">
        <div class="metric-heading"><span>Temperature</span><span class="metric-icon" aria-hidden="true">♨</span></div>
        <strong>${formatTemperature(s.temperature_celsius)}</strong>
        ${meter(isNumber(s.temperature_celsius) ? s.temperature_celsius / 85 * 100 : 0)}
        <small class="hardware-health hardware-health--${hardwareClass}">${hardwareLabel}</small>
      </article>

      <article class="metric-card metric-card--cyan">
        <div class="metric-heading"><span>Network</span><span class="metric-icon" aria-hidden="true">↕</span></div>
        <div class="split-value">
          <span><b>↓</b> ${formatRate(s.network_received_bytes_per_second)}</span>
          <span><b>↑</b> ${formatRate(s.network_transmitted_bytes_per_second)}</span>
        </div>
        <small>All physical interfaces</small>
      </article>

      <article class="metric-card metric-card--lime">
        <div class="metric-heading"><span>Load average</span><span class="metric-icon" aria-hidden="true">≈</span></div>
        <strong>${formatNumber(s.load_one)}</strong>
        <div class="load-values">
          <span>5m <b>${formatNumber(s.load_five)}</b></span>
          <span>15m <b>${formatNumber(s.load_fifteen)}</b></span>
        </div>
        <small>1 minute average</small>
      </article>
    </div>

    <div class="detail-grid">
      <article class="panel">
        <div class="panel-heading">
          <div><p class="eyebrow">Storage</p><h2>Mounted disks</h2></div>
          <span class="count-badge">${filesystems.length}</span>
        </div>
        <div class="disk-list">${disks}</div>
      </article>

      <article class="panel">
        <div class="panel-heading">
          <div><p class="eyebrow">Interfaces</p><h2>Network activity</h2></div>
        </div>
        <div class="interface-list">${interfaceRows}</div>
      </article>
    </div>

    <article class="panel database-panel">
      <div class="panel-heading">
        <div><p class="eyebrow">Storage</p><h2>Databases</h2></div>
        <span class="count-badge">${databases.length}</span>
      </div>
      <div class="database-list">${databaseRows}</div>
    </article>

    <article class="panel process-panel">
      <div class="panel-heading">
        <div><p class="eyebrow">Processes</p><h2>Top resource consumers</h2></div>
        <span class="count-badge">${pluralize(s.logged_in_users || 0, "user")} connected</span>
      </div>
      <div class="process-columns">
        <div>
          <h3>CPU</h3>
          <div class="process-list">${processRows(topCpu, (p) => formatPercentage(p.cpu_usage), "CPU rankings need two samples.")}</div>
        </div>
        <div>
          <h3>Memory</h3>
          <div class="process-list">${processRows(topMemory, (p) => formatBytes(p.memory_bytes), "Process memory data is not available yet.")}</div>
        </div>
      </div>
    </article>`
}

// Updates the "Last sample … ago" label and flips to stale every second.
function refreshFreshness() {
  const sample = state.latest
  if (!sample) return

  const time = document.querySelector("[data-sample-time]")
  if (time) time.textContent = `Last sample ${ageLabel(sampleAge(sample))} ago`

  if (sample.status !== "error" && sampleAge(sample) >= state.freshnessThreshold) {
    const status = document.querySelector("[data-status]")
    const label = document.querySelector("[data-status-label]")
    if (status) status.className = "status status--stale"
    if (label) label.textContent = "Data is stale"
  }
}

// ---- Charts ----------------------------------------------------------------

function timeLabel(timestamp) {
  return new Date(timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

function chartBytes(value) {
  const amount = Number(value) || 0
  if (amount < 1024) return `${Math.round(amount)} B`

  const units = ["KB", "MB", "GB", "TB"]
  const index = Math.min(Math.floor(Math.log(amount) / Math.log(1024)) - 1, units.length - 1)
  return `${(amount / (1024 ** (index + 1))).toFixed(1)} ${units[index]}`
}

function dataset(label, data, color) {
  return {
    label,
    data,
    borderColor: color,
    backgroundColor: `${color}1f`,
    borderWidth: 2,
    pointRadius: 0,
    pointHoverRadius: 4,
    tension: 0.3,
    spanGaps: true,
    fill: true
  }
}

function chartOptions({ min, max, suffix, bytes } = {}) {
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    interaction: { intersect: false, mode: "index" },
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: "#171923",
        borderColor: "rgba(255, 255, 255, .12)",
        borderWidth: 1,
        padding: 12,
        callbacks: {
          label: (context) => {
            const value = context.parsed.y
            return `${context.dataset.label}: ${bytes ? chartBytes(value) + "/s" : value + (suffix || "")}`
          }
        }
      }
    },
    scales: {
      x: {
        grid: { display: false },
        ticks: { color: "#7e8496", maxTicksLimit: 6, maxRotation: 0 }
      },
      y: {
        min,
        max,
        grid: { color: "rgba(255, 255, 255, .06)" },
        ticks: {
          color: "#7e8496",
          callback: (value) => bytes ? chartBytes(value) : `${value}${suffix || ""}`
        }
      }
    }
  }
}

// Each chart lists the point fields it draws, in dataset order.
const CHARTS = [
  {
    canvas: "chart-resources",
    options: { min: 0, max: 100, suffix: "%" },
    series: [["CPU", "cpu_usage", "#ff725e"], ["Memory", "memory_usage", "#9b8afb"], ["Swap", "swap_usage", "#50c9ff"]]
  },
  {
    canvas: "chart-load",
    options: { min: 0 },
    series: [["1 minute", "load_one", "#c8f465"], ["5 minutes", "load_five", "#70d6c7"]]
  },
  {
    canvas: "chart-network",
    options: { min: 0, bytes: true },
    series: [["Received", "network_received", "#50c9ff"], ["Sent", "network_transmitted", "#ffb15e"]]
  },
  {
    canvas: "chart-temperature",
    options: { min: 0, max: 85, suffix: "°C" },
    series: [["Temperature", "temperature_celsius", "#ffb15e"]]
  }
]

function buildCharts(points) {
  state.charts.forEach((chart) => chart.destroy())
  if (typeof Chart === "undefined") {
    state.charts = []
    return
  }

  state.charts = CHARTS.map((config) => new Chart(document.getElementById(config.canvas), {
    type: "line",
    data: {
      labels: points.map((point) => timeLabel(point.sampled_at)),
      datasets: config.series.map(([label, field, color]) => dataset(label, points.map((point) => point[field]), color))
    },
    options: chartOptions(config.options)
  }))
}

function appendPoint(point) {
  state.charts.forEach((chart, index) => {
    chart.data.labels.push(timeLabel(point.sampled_at))
    chart.data.datasets.forEach((set, setIndex) => set.data.push(point[CHARTS[index].series[setIndex][1]]))
    compact(chart)
    chart.update("none")
  })
}

// Halves the resolution of older points once a chart grows past
// MAX_CHART_POINTS, keeping the most recent points at full resolution.
function compact(chart) {
  if (chart.data.labels.length <= MAX_CHART_POINTS) return

  const oldLength = chart.data.labels.length - KEEP_RECENT_POINTS
  const retained = Array.from({ length: oldLength }, (_, index) => index)
    .filter((index) => index % 2 === 0)
    .concat(Array.from({ length: KEEP_RECENT_POINTS }, (_, index) => oldLength + index))

  chart.data.labels = retained.map((index) => chart.data.labels[index])
  chart.data.datasets.forEach((set) => {
    set.data = retained.map((index) => set.data[index])
  })
}

// ---- Loading and live updates ---------------------------------------------

async function load() {
  const response = await fetch("/api/dashboard", { cache: "no-store" })
  if (!response.ok) throw new Error(`Dashboard request failed: ${response.status}`)
  const data = await response.json()

  state.freshnessThreshold = data.freshness_threshold_seconds
  state.latest = data.latest
  renderMetrics(data.latest)
  buildCharts(data.history)
  document.getElementById("footer").textContent =
    `PiPulse keeps raw samples for ${data.retention_hours} hours · Refresh interval ${data.interval_seconds} seconds`
}

function connect() {
  const stream = new EventSource("/events")
  state.stream = stream

  stream.onmessage = (event) => {
    const { sample, point } = JSON.parse(event.data)
    state.latest = sample
    renderMetrics(sample)
    if (point) appendPoint(point)
  }

  // After a dropped connection, reload everything so the charts have no gap.
  stream.onerror = () => { state.reloadOnOpen = true }
  stream.onopen = () => {
    if (!state.reloadOnOpen) return
    state.reloadOnOpen = false
    load().catch((error) => console.error(error))
  }
}

document.addEventListener("DOMContentLoaded", () => {
  load()
    .catch((error) => console.error(error))
    .finally(connect)
  window.setInterval(refreshFreshness, 1000)
})
