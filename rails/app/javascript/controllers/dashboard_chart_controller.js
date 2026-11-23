import { Controller } from "@hotwired/stimulus"
import { Chart, registerables } from "chart.js"

Chart.register(...registerables)

export default class extends Controller {
  static targets = ["resources", "load", "temperature", "network", "samples"]
  static values = { history: Array }

  connect() {
    const points = this.historyValue || []
    this.charts = [
      this.buildResourcesChart(points),
      this.buildLoadChart(points),
      this.buildTemperatureChart(points),
      this.buildNetworkChart(points)
    ]

    this.observer = new MutationObserver(() => this.consumeLiveSamples())
    this.observer.observe(this.samplesTarget, { childList: true })
    this.consumeLiveSamples()
  }

  disconnect() {
    this.observer?.disconnect()
    this.charts?.forEach((chart) => chart.destroy())
  }

  buildResourcesChart(points) {
    return new Chart(this.resourcesTarget, {
      type: "line",
      data: {
        labels: points.map((point) => this.label(point.sampled_at)),
        datasets: [
          this.dataset("CPU", points.map((point) => point.cpu_usage), "#ff725e"),
          this.dataset("Memory", points.map((point) => point.memory_usage), "#9b8afb"),
          this.dataset("Swap", points.map((point) => point.swap_usage), "#50c9ff")
        ]
      },
      options: this.options({ min: 0, max: 100, suffix: "%" })
    })
  }

  buildLoadChart(points) {
    return new Chart(this.loadTarget, {
      type: "line",
      data: {
        labels: points.map((point) => this.label(point.sampled_at)),
        datasets: [
          this.dataset("1 minute", points.map((point) => point.load_one), "#c8f465"),
          this.dataset("5 minutes", points.map((point) => point.load_five), "#70d6c7")
        ]
      },
      options: this.options({ min: 0 })
    })
  }

  buildNetworkChart(points) {
    return new Chart(this.networkTarget, {
      type: "line",
      data: {
        labels: points.map((point) => this.label(point.sampled_at)),
        datasets: [
          this.dataset("Received", points.map((point) => point.network_received), "#50c9ff"),
          this.dataset("Sent", points.map((point) => point.network_transmitted), "#ffb15e")
        ]
      },
      options: this.options({ min: 0, bytes: true })
    })
  }

  buildTemperatureChart(points) {
    return new Chart(this.temperatureTarget, {
      type: "line",
      data: {
        labels: points.map((point) => this.label(point.sampled_at)),
        datasets: [
          this.dataset("Temperature", points.map((point) => point.temperature_celsius), "#ffb15e")
        ]
      },
      options: this.options({ min: 0, max: 85, suffix: "°C" })
    })
  }

  dataset(label, data, color) {
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

  options({ min, max, suffix, bytes } = {}) {
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
              return `${context.dataset.label}: ${bytes ? this.humanBytes(value) + "/s" : value + (suffix || "")}`
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
            callback: (value) => bytes ? this.humanBytes(value) : `${value}${suffix || ""}`
          }
        }
      }
    }
  }

  consumeLiveSamples() {
    this.samplesTarget.querySelectorAll("[data-live-sample]").forEach((element) => {
      const point = JSON.parse(element.dataset.liveSample)
      this.appendPoint(point)
      element.remove()
    })
  }

  appendPoint(point) {
    const label = this.label(point.sampled_at)
    const values = [
      [point.cpu_usage, point.memory_usage, point.swap_usage],
      [point.load_one, point.load_five],
      [point.temperature_celsius],
      [point.network_received, point.network_transmitted]
    ]

    this.charts.forEach((chart, index) => {
      chart.data.labels.push(label)
      chart.data.datasets.forEach((dataset, datasetIndex) => {
        dataset.data.push(values[index][datasetIndex])
      })
      this.compact(chart)
      chart.update("none")
    })
  }

  compact(chart) {
    if (chart.data.labels.length <= 720) return

    const keepRecent = 120
    const oldLength = chart.data.labels.length - keepRecent
    const retainedIndexes = Array.from({ length: oldLength }, (_, index) => index)
      .filter((index) => index % 2 === 0)
      .concat(Array.from({ length: keepRecent }, (_, index) => oldLength + index))

    chart.data.labels = retainedIndexes.map((index) => chart.data.labels[index])
    chart.data.datasets.forEach((dataset) => {
      dataset.data = retainedIndexes.map((index) => dataset.data[index])
    })
  }

  label(timestamp) {
    return new Date(timestamp).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  }

  humanBytes(value) {
    const amount = Number(value) || 0
    if (amount < 1024) return `${Math.round(amount)} B`

    const units = ["KB", "MB", "GB", "TB"]
    const index = Math.min(Math.floor(Math.log(amount) / Math.log(1024)) - 1, units.length - 1)
    return `${(amount / (1024 ** (index + 1))).toFixed(1)} ${units[index]}`
  }
}
