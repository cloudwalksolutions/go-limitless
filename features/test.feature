
@health
Feature: Health Checks

  Scenario Outline: API health check
    When I send "<method>" request to "<endpoint>"
    Then the response code should be <status>

    Examples:
      | method | endpoint | status |
      | GET    | health  | 200    |
