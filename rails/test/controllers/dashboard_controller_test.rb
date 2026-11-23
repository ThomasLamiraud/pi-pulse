require "test_helper"

class DashboardControllerTest < ActionDispatch::IntegrationTest
  test "renders the live dashboard" do
    get root_url

    assert_response :success
    assert_select "h1", "PiPulse"
    assert_select "#dashboard_metrics"
    assert_select "[data-controller='dashboard-chart']"
    assert_select "turbo-cable-stream-source"
  end
end
