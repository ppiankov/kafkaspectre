# Contributing to KafkaSpectre

Thank you for your interest in contributing to KafkaSpectre! We appreciate your help in making this project better.

## How Can I Contribute?

There are several ways you can contribute:

*   **Report Bugs:** If you find a bug, please open an issue on GitHub.
*   **Suggest Features:** Have a great idea for a new feature? Open an issue to discuss it.
*   **Submit Pull Requests:** Directly contribute code, documentation, or other improvements.

## Getting Started

### Prerequisites

To get started with developing KafkaSpectre, you'll need the following:

*   **Go:** Version 1.25.x or later. You can download it from [golang.org](https://golang.org/dl/).
*   **Kafka:** A running Kafka cluster for integration tests. You can set one up using Docker or a local installation.

### Build, Test, and Lint

To ensure your changes are working correctly and adhere to our coding standards, please run the following commands:

*   **Build:** `make build`
*   **Test:** `make test`
*   **Lint:** `golangci-lint run` (You might need to install `golangci-lint` if you don't have it: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.57.1`)

### Pull Request Conventions

When submitting a pull request, please follow these guidelines:

*   **Branch:** Create a new branch for your changes (e.g., `feature/my-new-feature` or `bugfix/fix-for-issue-123`).
*   **Atomic Commits:** Break down your changes into small, atomic commits that address a single concern.
*   **Clear Commit Messages:** Write clear, concise, and descriptive commit messages.
    *   Start with a verb in the imperative mood (e.g., "Fix:", "Add:", "Update:").
    *   Include a brief summary (first line, 50-72 characters).
    *   Optionally, provide a more detailed explanation in the commit body.
*   **Tests:** Include unit and/or integration tests for your changes.
*   **Documentation:** Update relevant documentation if your changes affect how KafkaSpectre is used or configured.
*   **One Feature/Bug Per PR:** Each pull request should address a single feature or bug.
*   **Rebase:** Rebase your branch on top of the latest `main` branch before submitting to keep a clean history.

### Architecture Overview

KafkaSpectre is structured as follows:

*   **`cmd/kafkaspectre`**: Contains the main CLI application entry point and Cobra command wiring.
*   **`internal/config`**: Handles application configuration, including loading from files and CLI flags.
*   **`internal/kafka`**: Contains logic for interacting with Kafka clusters (e.g., inspectors, connection management).
*   **`internal/logging`**: Provides structured logging capabilities.
*   **`internal/reporter`**: Manages report generation in various formats (text, JSON, SARIF).
*   **`internal/scanner`**: Implements code scanning functionality to find Kafka topic references.

We look forward to your contributions!