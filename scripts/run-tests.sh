#!/bin/bash

# Comprehensive test runner for SIPREC server
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_RESULTS_DIR="${PROJECT_ROOT}/test-results"
COVERAGE_DIR="${TEST_RESULTS_DIR}/coverage"
REPORTS_DIR="${TEST_RESULTS_DIR}/reports"

# Default test configuration
RUN_UNIT_TESTS=true
RUN_INTEGRATION_TESTS=true
RUN_LOAD_TESTS=false
RUN_E2E_TESTS=false
GENERATE_COVERAGE=true
GENERATE_REPORTS=true
PARALLEL_TESTS=true
VERBOSE=false
TIMEOUT="5m"

# Test environment variables
export GO_ENV=test
export LOG_LEVEL=warn
export TEST_TIMEOUT=${TIMEOUT}

print_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -u, --unit          Run unit tests only"
    echo "  -i, --integration   Run integration tests only"
    echo "  -l, --load          Run load tests"
    echo "  -e, --e2e           Run end-to-end tests"
    echo "  -a, --all           Run all tests (default)"
    echo "  -c, --coverage      Generate coverage report (default: true)"
    echo "  -r, --reports       Generate test reports (default: true)"
    echo "  -p, --parallel      Run tests in parallel (default: true)"
    echo "  -v, --verbose       Verbose output"
    echo "  -t, --timeout TIME  Test timeout (default: 5m)"
    echo "  --no-coverage       Skip coverage generation"
    echo "  --no-reports        Skip report generation"
    echo "  --no-parallel       Run tests sequentially"
    echo "  -h, --help          Show this help"
    echo ""
    echo "Examples:"
    echo "  $0 --unit --verbose"
    echo "  $0 --integration --timeout 10m"
    echo "  $0 --all --no-coverage"
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -u|--unit)
                RUN_UNIT_TESTS=true
                RUN_INTEGRATION_TESTS=false
                RUN_LOAD_TESTS=false
                RUN_E2E_TESTS=false
                shift
                ;;
            -i|--integration)
                RUN_UNIT_TESTS=false
                RUN_INTEGRATION_TESTS=true
                RUN_LOAD_TESTS=false
                RUN_E2E_TESTS=false
                shift
                ;;
            -l|--load)
                RUN_LOAD_TESTS=true
                shift
                ;;
            -e|--e2e)
                RUN_E2E_TESTS=true
                shift
                ;;
            -a|--all)
                RUN_UNIT_TESTS=true
                RUN_INTEGRATION_TESTS=true
                RUN_LOAD_TESTS=true
                RUN_E2E_TESTS=true
                shift
                ;;
            -c|--coverage)
                GENERATE_COVERAGE=true
                shift
                ;;
            --no-coverage)
                GENERATE_COVERAGE=false
                shift
                ;;
            -r|--reports)
                GENERATE_REPORTS=true
                shift
                ;;
            --no-reports)
                GENERATE_REPORTS=false
                shift
                ;;
            -p|--parallel)
                PARALLEL_TESTS=true
                shift
                ;;
            --no-parallel)
                PARALLEL_TESTS=false
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -t|--timeout)
                TIMEOUT="$2"
                export TEST_TIMEOUT=${TIMEOUT}
                shift 2
                ;;
            -h|--help)
                print_usage
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                print_usage
                exit 1
                ;;
        esac
    done
}

print_header() {
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE}           SIPREC Server Test Suite            ${NC}"
    echo -e "${BLUE}================================================${NC}"
    echo ""
}

print_section() {
    echo -e "${YELLOW}>>> $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

setup_test_environment() {
    print_section "Setting up test environment"
    
    # Create test directories
    mkdir -p "${TEST_RESULTS_DIR}"
    mkdir -p "${COVERAGE_DIR}"
    mkdir -p "${REPORTS_DIR}"
    mkdir -p "${PROJECT_ROOT}/test-recordings"
    
    # Clean previous results
    rm -rf "${TEST_RESULTS_DIR}"/*
    
    # Set Go test environment
    export CGO_ENABLED=1
    export GOCACHE="${TEST_RESULTS_DIR}/.gocache"
    
    # Create test data if needed
    if [[ ! -f "${PROJECT_ROOT}/test-recordings/test_audio.wav" ]]; then
        print_info "Creating synthetic test audio data"
        create_test_audio
    fi
    
    print_success "Test environment setup complete"
}

create_test_audio() {
    # Create a simple WAV file for testing
    local test_audio="${PROJECT_ROOT}/test-recordings/test_audio.wav"
    
    # Simple WAV header + minimal audio data
    cat > "${test_audio}" << 'EOF'
RIFF$WAVEfmt           @"data
EOF
    
    # Add some bytes to make it a valid minimal WAV
    printf '\x52\x49\x46\x46\x24\x00\x00\x00\x57\x41\x56\x45\x66\x6D\x74\x20\x10\x00\x00\x00\x01\x00\x01\x00\x40\x1F\x00\x00\x80\x3E\x00\x00\x02\x00\x10\x00\x64\x61\x74\x61\x00\x00\x00\x00' > "${test_audio}"
}

check_dependencies() {
    print_section "Checking dependencies"
    
    # Check Go version
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed"
        exit 1
    fi
    
    local go_version=$(go version | cut -d' ' -f3)
    print_info "Go version: ${go_version}"
    
    # Check if required packages are available
    if ! go mod verify &> /dev/null; then
        print_info "Downloading Go modules"
        go mod download
    fi
    
    # Check for test tools
    if ! command -v gotestsum &> /dev/null && [[ "${GENERATE_REPORTS}" == "true" ]]; then
        print_info "Installing gotestsum for better test reporting"
        go install gotest.tools/gotestsum@latest
    fi
    
    print_success "Dependencies check complete"
}

run_unit_tests() {
    if [[ "${RUN_UNIT_TESTS}" != "true" ]]; then
        return 0
    fi
    
    print_section "Running unit tests"
    
    local test_args=()
    test_args+=("-race")
    test_args+=("-timeout" "${TIMEOUT}")
    
    if [[ "${VERBOSE}" == "true" ]]; then
        test_args+=("-v")
    fi
    
    if [[ "${PARALLEL_TESTS}" == "true" ]]; then
        test_args+=("-parallel" "4")
    fi
    
    if [[ "${GENERATE_COVERAGE}" == "true" ]]; then
        test_args+=("-coverprofile=${COVERAGE_DIR}/unit.out")
        test_args+=("-covermode=atomic")
    fi
    
    local packages=(
        "./pkg/..."
        "./cmd/..."
        "./test/unit/..."
    )
    
    if command -v gotestsum &> /dev/null && [[ "${GENERATE_REPORTS}" == "true" ]]; then
        gotestsum --format testname --junitfile "${REPORTS_DIR}/unit-tests.xml" -- "${test_args[@]}" "${packages[@]}"
    else
        go test "${test_args[@]}" "${packages[@]}"
    fi
    
    local exit_code=$?
    if [[ $exit_code -eq 0 ]]; then
        print_success "Unit tests passed"
    else
        print_error "Unit tests failed"
        return $exit_code
    fi
}

run_integration_tests() {
    if [[ "${RUN_INTEGRATION_TESTS}" != "true" ]]; then
        return 0
    fi
    
    print_section "Running integration tests"
    
    # Start required services for integration tests
    if command -v docker-compose &> /dev/null; then
        print_info "Starting test services with Docker Compose"
        docker-compose -f docker-compose.dev.yml up -d rabbitmq redis
        
        # Wait for services to be ready
        sleep 10
        
        # Set test environment variables
        export TEST_AMQP_URL="amqp://guest:guest@localhost:5672/"
        export TEST_REDIS_URL="redis://localhost:6379"
    fi
    
    local test_args=()
    test_args+=("-race")
    test_args+=("-timeout" "${TIMEOUT}")
    test_args+=("-tags=integration")
    
    if [[ "${VERBOSE}" == "true" ]]; then
        test_args+=("-v")
    fi
    
    if [[ "${GENERATE_COVERAGE}" == "true" ]]; then
        test_args+=("-coverprofile=${COVERAGE_DIR}/integration.out")
        test_args+=("-covermode=atomic")
    fi
    
    local packages=(
        "./test/integration/..."
    )
    
    if command -v gotestsum &> /dev/null && [[ "${GENERATE_REPORTS}" == "true" ]]; then
        gotestsum --format testname --junitfile "${REPORTS_DIR}/integration-tests.xml" -- "${test_args[@]}" "${packages[@]}"
    else
        go test "${test_args[@]}" "${packages[@]}"
    fi
    
    local exit_code=$?
    
    # Cleanup services
    if command -v docker-compose &> /dev/null; then
        docker-compose -f docker-compose.dev.yml down
    fi
    
    if [[ $exit_code -eq 0 ]]; then
        print_success "Integration tests passed"
    else
        print_error "Integration tests failed"
        return $exit_code
    fi
}

run_load_tests() {
    if [[ "${RUN_LOAD_TESTS}" != "true" ]]; then
        return 0
    fi
    
    print_section "Running load tests"
    
    local test_args=()
    test_args+=("-bench=.")
    test_args+=("-benchmem")
    test_args+=("-timeout" "${TIMEOUT}")
    
    if [[ "${VERBOSE}" == "true" ]]; then
        test_args+=("-v")
    fi
    
    local packages=(
        "./pkg/..."
        "./test/unit/..."
        "./test/integration/..."
    )
    
    # Run benchmarks and save results
    go test "${test_args[@]}" "${packages[@]}" | tee "${REPORTS_DIR}/benchmark-results.txt"
    
    local exit_code=$?
    if [[ $exit_code -eq 0 ]]; then
        print_success "Load tests completed"
    else
        print_error "Load tests failed"
        return $exit_code
    fi
}

run_e2e_tests() {
    if [[ "${RUN_E2E_TESTS}" != "true" ]]; then
        return 0
    fi
    
    print_section "Running end-to-end tests"
    
    # Build the application
    print_info "Building application for E2E tests"
    go build -o "${TEST_RESULTS_DIR}/siprec-test" ./cmd/siprec
    
    # Start the application in background
    print_info "Starting SIPREC server for E2E tests"
    export HTTP_PORT=8081
    export SIP_PORT=5061
    export LOG_LEVEL=error
    
    "${TEST_RESULTS_DIR}/siprec-test" &
    local app_pid=$!
    
    # Wait for application to start
    sleep 5
    
    # Run E2E tests
    local test_args=()
    test_args+=("-timeout" "${TIMEOUT}")
    test_args+=("-tags=e2e")
    
    if [[ "${VERBOSE}" == "true" ]]; then
        test_args+=("-v")
    fi
    
    local packages=(
        "./test/e2e/..."
    )
    
    if command -v gotestsum &> /dev/null && [[ "${GENERATE_REPORTS}" == "true" ]]; then
        gotestsum --format testname --junitfile "${REPORTS_DIR}/e2e-tests.xml" -- "${test_args[@]}" "${packages[@]}"
    else
        go test "${test_args[@]}" "${packages[@]}"
    fi
    
    local exit_code=$?
    
    # Cleanup
    kill $app_pid 2>/dev/null || true
    
    if [[ $exit_code -eq 0 ]]; then
        print_success "End-to-end tests passed"
    else
        print_error "End-to-end tests failed"
        return $exit_code
    fi
}

generate_coverage_report() {
    if [[ "${GENERATE_COVERAGE}" != "true" ]]; then
        return 0
    fi
    
    print_section "Generating coverage report"
    
    # Merge coverage files if multiple exist
    local coverage_files=(${COVERAGE_DIR}/*.out)
    if [[ ${#coverage_files[@]} -gt 1 ]]; then
        print_info "Merging coverage files"
        echo "mode: atomic" > "${COVERAGE_DIR}/merged.out"
        for file in "${coverage_files[@]}"; do
            if [[ "$(basename "$file")" != "merged.out" ]]; then
                tail -n +2 "$file" >> "${COVERAGE_DIR}/merged.out"
            fi
        done
        local coverage_file="${COVERAGE_DIR}/merged.out"
    elif [[ ${#coverage_files[@]} -eq 1 ]]; then
        local coverage_file="${coverage_files[0]}"
    else
        print_info "No coverage files found"
        return 0
    fi
    
    # Generate HTML report
    go tool cover -html="${coverage_file}" -o "${REPORTS_DIR}/coverage.html"
    
    # Generate text summary
    go tool cover -func="${coverage_file}" > "${REPORTS_DIR}/coverage.txt"
    
    # Print coverage summary
    local total_coverage=$(go tool cover -func="${coverage_file}" | grep total | awk '{print $3}')
    print_info "Total coverage: ${total_coverage}"
    
    print_success "Coverage report generated"
}

generate_test_reports() {
    if [[ "${GENERATE_REPORTS}" != "true" ]]; then
        return 0
    fi
    
    print_section "Generating test reports"
    
    # Create test summary
    local summary_file="${REPORTS_DIR}/test-summary.txt"
    cat > "${summary_file}" << EOF
SIPREC Server Test Results
=========================
Generated: $(date)
Test Timeout: ${TIMEOUT}
Parallel Tests: ${PARALLEL_TESTS}

Test Categories:
- Unit Tests: ${RUN_UNIT_TESTS}
- Integration Tests: ${RUN_INTEGRATION_TESTS}
- Load Tests: ${RUN_LOAD_TESTS}
- E2E Tests: ${RUN_E2E_TESTS}

Coverage Generated: ${GENERATE_COVERAGE}
EOF
    
    # Add coverage summary if available
    if [[ -f "${REPORTS_DIR}/coverage.txt" ]]; then
        echo "" >> "${summary_file}"
        echo "Coverage Summary:" >> "${summary_file}"
        echo "=================" >> "${summary_file}"
        tail -1 "${REPORTS_DIR}/coverage.txt" >> "${summary_file}"
    fi
    
    print_success "Test reports generated in ${REPORTS_DIR}"
}

main() {
    cd "${PROJECT_ROOT}"
    
    parse_args "$@"
    print_header
    
    # Setup
    setup_test_environment
    check_dependencies
    
    # Run tests
    local overall_exit_code=0
    
    run_unit_tests || overall_exit_code=$?
    run_integration_tests || overall_exit_code=$?
    run_load_tests || overall_exit_code=$?
    run_e2e_tests || overall_exit_code=$?
    
    # Generate reports
    generate_coverage_report
    generate_test_reports
    
    # Final summary
    echo ""
    if [[ $overall_exit_code -eq 0 ]]; then
        print_success "All tests passed successfully!"
        print_info "Results available in: ${TEST_RESULTS_DIR}"
    else
        print_error "Some tests failed (exit code: $overall_exit_code)"
        print_info "Check results in: ${TEST_RESULTS_DIR}"
    fi
    
    exit $overall_exit_code
}

# Run main function
main "$@"