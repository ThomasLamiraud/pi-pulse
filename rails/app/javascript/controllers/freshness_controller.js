import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static targets = ["status", "time", "label"]
  static values = { sampledAt: String, error: Boolean, threshold: Number }

  connect() {
    if (!this.hasSampledAtValue) return

    this.refresh()
    this.timer = window.setInterval(() => this.refresh(), 1000)
  }

  disconnect() {
    window.clearInterval(this.timer)
  }

  refresh() {
    const age = Math.max(0, (Date.now() - Date.parse(this.sampledAtValue)) / 1000)
    this.timeTarget.textContent = `Last sample ${this.ageLabel(age)} ago`

    if (age >= this.thresholdValue && !this.errorValue) {
      this.statusTarget.className = "status status--stale"
      this.labelTarget.textContent = "Data is stale"
    }
  }

  ageLabel(seconds) {
    if (seconds < 60) return `${Math.floor(seconds)} seconds`
    if (seconds < 3600) return `${Math.floor(seconds / 60)} minutes`

    return `${Math.floor(seconds / 3600)} hours`
  }
}
