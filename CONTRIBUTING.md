# Contributing to Shelf 🛠️

First off, thank you for considering contributing to Shelf! This project is built on extreme simplicity, zero dependencies, and high performance. We want to keep it that way, and your help is highly appreciated.

## How to get started

### 1. The Core Philosophy
Before you write any code, please understand the golden rule of Shelf: **Zero External Dependencies.**

We will reject any PR that adds a third-party library to `go.mod` (e.g., `github.com/gorilla/mux`, `github.com/prometheus/client_golang`, etc.). We believe the Go standard library is incredibly powerful and we want to prove it.

### 2. Local Setup
Getting Shelf running on your machine takes less than a minute.

```bash
# Clone the repository
git clone https://github.com/yourusername/shelf.git
cd shelf

# Run the tests
make test

# Build the binaries
make build
```

### 3. Testing Your Changes
Shelf has two layers of testing. You must ensure both pass before submitting a PR.

1.  **Unit Tests (Go):** Run `make test` to execute the standard library test suite with the `-race` detector enabled.
2.  **Integration Tests (Bash/Docker):** We have a full end-to-end pipeline test that spins up the cluster, routes traffic, and validates the telemetry pipeline.
    ```bash
    # Run the native integration test
    ./integration_test.sh

    # Run the Docker Compose integration test
    ./docker_integration_test.sh
    ```

---

## Finding something to work on

If you're looking for a place to start, check out the [Issues](https://github.com/yourusername/shelf/issues) tab. Look for issues labeled:
- `good first issue`: Perfect for newcomers to the codebase.
- `help wanted`: Features or bugs we specifically need assistance with.
---

## Submitting a Pull Request (PR)

1. **Fork the repository.**
2. **Create a branch** for your feature or bugfix (`git checkout -b feature/my-awesome-feature`).
3. **Write your code.** Keep it simple. Stick to standard Go formatting (`go fmt`).
4. **Write tests.** If you add a new API route, write a test in `main_test.go`. If you add a new metric, test it in `metrics_test.go`.
5. **Run the integration scripts** (`./integration_test.sh` and `./docker_integration_test.sh`) to ensure you didn't break the pipeline.
6. **Commit your changes.** Use clear, concise commit messages (e.g., `feat(query): add max() aggregation`).
7. **Open a PR** against the `main` branch.

We will review your PR as quickly as possible. Don't worry if we ask for changes—it's just part of the process to keep the codebase clean and fast!

---

## Found a Bug?
If you find a bug, please open an Issue. Include:
1. What you did.
2. What you expected to happen.
3. What actually happened (with logs or stack traces if possible).

---
Thank you for making Shelf better!
