PYTHON ?= python3
export PYTHONDONTWRITEBYTECODE := 1

.PHONY: setup doctor dev dev-live migrate test build frontend test-scripts e2e e2e-mock e2e-privacy e2e-controls e2e-recovery e2e-redis e2e-dependencies e2e-embedded e2e-abort e2e-browser e2e-vite-proxy start stop status diagnose probe-dev verify-config-refactor verify-port8081 verify-final
setup doctor dev dev-live migrate test start stop status diagnose probe-dev verify-config-refactor verify-port8081 verify-final:
	$(PYTHON) scripts/local.py $@

frontend:
	$(PYTHON) scripts/local.py frontend

build: frontend
	$(PYTHON) scripts/local.py build

test-scripts:
	$(PYTHON) tests/e2e/test_local.py

e2e:
	$(PYTHON) tests/e2e/run.py all

e2e-browser:
	$(PYTHON) tests/e2e/browser/run_browser.py

e2e-vite-proxy:
	$(PYTHON) tests/e2e/vite_proxy.py

e2e-mock e2e-privacy e2e-controls e2e-recovery e2e-redis e2e-dependencies e2e-embedded e2e-abort:
	$(PYTHON) tests/e2e/run.py $(patsubst e2e-%,%,$@)
