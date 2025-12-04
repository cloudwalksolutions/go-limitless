<h1 align="center">go-limitless</h1>

<p align="center">
  <strong>A BDD testing framework for Go APIs</strong>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go" alt="Go Version"></a>
  <a href="/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
  <a href="https://goreportcard.com/report/github.com/cloudwalksolutions/go-limitless"><img src="https://goreportcard.com/badge/github.com/cloudwalksolutions/go-limitless" alt="Go Report Card"></a>
</p>

---

## About

**go-limitless** is a BDD (Behavior-Driven Development) testing library for Go that enables API testing using human-readable Gherkin feature specifications. Built on top of [godog](https://github.com/cucumber/godog), it provides a comprehensive set of step definitions for HTTP request/response testing, making it easy to write expressive integration tests for your APIs.

## Features

- **Full HTTP Support** - GET, POST, PUT, PATCH, DELETE methods
- **Request Payloads** - JSON bodies and query parameters
- **Response Assertions** - Status codes, JSON content, partial matching
- **JSON Path Queries** - Navigate nested response structures with dot notation
- **Variable Interpolation** - Store and reuse values across steps with `${variable}` syntax
- **Multi-Environment** - Built-in support for local, staging, and production environments
- **Bearer Token Auth** - Automatic authentication header injection
- **Structured Logging** - Debug output with zerolog

## Installation

```bash
go get github.com/cloudwalksolutions/go-limitless
```

## Quick Start

### 1. Create a test file

```go
// server_test.go
package main

import (
    "testing"
    "github.com/cloudwalksolutions/go-limitless/src/fixture"
)

func TestFeatures(t *testing.T) {
    f := fixture.NewServerFixture(nil)
    f.Run(&testing.M{})
}
```

### 2. Create a feature file

```gherkin
# features/api.feature
Feature: API Tests

  Scenario: Health check returns OK
    When I send "GET" request to "health"
    Then the response code should be 200

  Scenario: Create and retrieve a user
    When I send "POST" request to "users" with data
      """
      {"name": "John Doe", "email": "john@example.com"}
      """
    Then the response code should be 201
    And the response should contain a "id"
    And I save "id" from the response

    When I send "GET" request to "users/${id}"
    Then the response code should be 200
    And the response should contain a "name" set to "John Doe"
```

### 3. Run tests

```bash
go test -v
```

## Step Definitions

### Request Steps

| Step | Description |
|------|-------------|
| `I send "METHOD" request to "endpoint"` | Send a request (GET, POST, DELETE) |
| `I send "METHOD" request to "endpoint" with data` | Send with JSON body (POST, PUT, PATCH) |
| `I send "METHOD" request to "endpoint" with params` | Send with query parameters |

### Response Status

| Step | Description |
|------|-------------|
| `the response code should be <code>` | Assert HTTP status code |
| `the response should not be empty` | Assert response has content |

### Response Content

| Step | Description |
|------|-------------|
| `the response should match json` | Exact JSON match |
| `the response should contain` | Partial content match (DocString) |
| `the response should contain a "key"` | Assert key exists |
| `the response should not contain a "key"` | Assert key doesn't exist |
| `the response should contain a "key" that contains items` | Assert array contains items |

### JSON Path Assertions

Use dot notation to query nested values (e.g., `data.user.name`).

| Step | Description |
|------|-------------|
| `the response should contain a "path" set to "value"` | Assert exact value |
| `the response should contain a "path" temporally equal to "value"` | Assert date/time equality |
| `the response should contain a "path" that is null` | Assert null value |
| `the response should contain a "path" that is not null` | Assert non-null value |
| `the response should contain a "path" that is empty` | Assert empty array/object |
| `the response should contain a "path" that is not empty` | Assert non-empty array/object |

### Array Assertions

| Step | Description |
|------|-------------|
| `the response should have a length of <n>` | Assert array length |
| `the response should contain a "path" with length <n>` | Assert nested array length |
| `the response should contain an item with "prop" set to "value"` | Find item by property |
| `the response should contain an item at index <n> with "prop" set to "value"` | Assert item at index |

### Data Extraction

| Step | Description |
|------|-------------|
| `I save "key" from the response` | Store value for later use |
| `I save the item at index <n> in "key" as "alias"` | Store array item |

## Variable Interpolation

Use `${variable}` syntax to inject dynamic values into requests:

| Variable | Description |
|----------|-------------|
| `${random_id}` | Random integer (0-9999999) |
| `${today}` | Current date (YYYY-MM-DD) |
| `${saved_key}` | Previously saved value |
| `${saved_key.property}` | Nested property from saved object |

### Example

```gherkin
Scenario: Create with random ID
  When I send "POST" request to "items" with data
    """
    {"external_id": "${random_id}", "date": "${today}"}
    """
  Then the response code should be 201
  And I save "id" from the response

  When I send "GET" request to "items/${id}"
  Then the response code should be 200
```

## Configuration

### Environment Variables

Create a `.env` file in your project root:

```env
DEBUG=true
LIFECYCLE=local
APP_DOMAIN=api.example.com
```

### Command-Line Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-v, --debug` | Enable debug logging | `false` |
| `-l, --lifecycle` | Environment (local/staging/prod) | `local` |

### URL Formation

URLs are automatically formatted based on lifecycle:

| Lifecycle | URL Pattern |
|-----------|-------------|
| `local` | `http://localhost:8080/api/{endpoint}` |
| `staging` | `https://staging.{appDomain}/api/{endpoint}` |
| `prod` | `https://{appDomain}/api/{endpoint}` |

## Example Feature File

```gherkin
@users
Feature: User Management API

  Scenario: Create a new user
    When I send "POST" request to "users" with data
      """
      {
        "name": "Jane Smith",
        "email": "jane@example.com",
        "role": "admin"
      }
      """
    Then the response code should be 201
    And the response should contain a "id"
    And the response should contain a "name" set to "Jane Smith"
    And the response should contain a "createdAt" that is not null
    And I save "id" from the response

  Scenario: List users with pagination
    When I send "GET" request to "users" with params
      """
      {"page": 1, "limit": 10}
      """
    Then the response code should be 200
    And the response should contain a "data" that is not empty
    And the response should contain a "meta.totalCount" that is not null

  Scenario: Update user
    When I send "PATCH" request to "users/${id}" with data
      """
      {"name": "Jane Doe"}
      """
    Then the response code should be 200
    And the response should contain a "name" set to "Jane Doe"

  Scenario: Delete user
    When I send "DELETE" request to "users/${id}"
    Then the response code should be 204
```

## License

MIT License - see [LICENSE](/LICENSE) for details.

## Author

[@walkerobrien-cw](https://github.com/walkerobrien-cw)
