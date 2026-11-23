class DashboardController < ApplicationController
  def index
    @sample = MetricSample.order(sampled_at: :desc).first
    @history = MetricSample.history_points.map(&:chart_payload)
  end
end
