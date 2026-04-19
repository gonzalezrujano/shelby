**Shelby = motor de observación y métricas**. Extrae datos, procesa, reporta. No despliega, no escribe externo.

### 🏗️ Shelby: Motor de Métricas AI-Driven

Agente ligero en **Go**. Lee fuentes (APIs, logs, sistema), consolida, reporta a UI local o cloud.

**Factor IA (YAML):** pipelines describen plan de recolección. LLM genera YAML al vuelo desde prompt del usuario.

---

### 🛠️ Arquitectura Go

1. **Collectors (solo lectura):** HTTP, sys_stat, log_read, etc. Interfaz `Collector`.
2. **Pipeline engine:** ejecuta `steps` en orden. Cada step produce `Output` tipado. Outputs guardados en contexto del pipeline, referenciables por siguientes steps vía `${steps.<id>.output.<campo>}`.
3. **Step types:**
   * `http_get`, `sys_stat`, `log_read` — collectors nativos Go.
   * `script` — ejecuta Node/Python/Rust externo. Input por stdin JSON, output stdout JSON.
   * `conditional` — branching según expresión sobre outputs previos.
   * `aggregator` — reduce/map/merge sobre outputs previos.
4. **Exporters:** servidor `net/http` local + cliente cloud opcional.
5. **Concurrencia:** goroutines por step independiente. Step dependiente espera output upstream.

---

### Ejemplo YAML

```yaml
name: "Monitor Salud Alpha"
interval: 60s
steps:
  - id: health
    type: http_get
    source: "https://api.ejemplo.com/health"
    extract: response_time

  - id: disk
    type: sys_stat
    source: "/dev/sda1"
    extract: percentage_used

  - id: enrich
    type: script
    runtime: python
    file: ./scripts/enrich.py
    input:
      latency: ${steps.health.output.response_time}
      disk: ${steps.disk.output.percentage_used}

  - id: alert
    type: conditional
    when: "${steps.enrich.output.score} > 80"
    then:
      - id: notify
        type: script
        runtime: node
        file: ./scripts/notify.js

output:
  latency: ${steps.health.output.response_time}
  disk: ${steps.disk.output.percentage_used}
  score: ${steps.enrich.output.score}

export:
  local_port: 8080
  cloud_endpoint: "https://cloud.shelby.io/ingest"
```

---

### 🖥️ CLI

Binario `shelby`. Comandos:

* `shelby list` — tabla pipelines registrados: nombre, intervalo, último run, status (ok/fail/running), próximo run.
* `shelby show <name>` — detalle pipeline, steps, últimos outputs.
* `shelby run <name>` — ejecuta ad-hoc, stream de logs por step.
* `shelby logs <name>` — historial de runs.
* `shelby add <file.yaml>` / `shelby rm <name>`.
* `shelby serve` — demonio + UI web en `localhost:8080`.

TUI con `bubbletea` o tabla simple `tablewriter`. Status con colores.

---

### Alcance Alpha
* ✅ Collector sistema (CPU/RAM/disk).
* ✅ Ejecutor pipeline con refs entre steps.
* ✅ Runner script externo (Node/Python/Rust).
* ✅ CLI `list` + `run` + `serve`.
* ❌ Escritura externa, S3, DBs.

---

### 📦 Structs Go (spec)

```go
package shelby

import "time"

type Pipeline struct {
    Name        string            `yaml:"name"`
    Description string            `yaml:"description,omitempty"`
    Interval    time.Duration     `yaml:"interval"`
    Steps       []Step            `yaml:"steps"`
    Output      map[string]string `yaml:"output,omitempty"` // key -> ref expr
    Export      ExportConfig      `yaml:"export,omitempty"`
}

type Step struct {
    ID      string                 `yaml:"id"`
    Type    StepType               `yaml:"type"`
    Source  string                 `yaml:"source,omitempty"`
    Extract string                 `yaml:"extract,omitempty"`

    // script
    Runtime string                 `yaml:"runtime,omitempty"` // node|python|rust|bash
    File    string                 `yaml:"file,omitempty"`
    Input   map[string]any         `yaml:"input,omitempty"`   // may contain ${refs}

    // conditional
    When    string                 `yaml:"when,omitempty"`    // expr
    Then    []Step                 `yaml:"then,omitempty"`
    Else    []Step                 `yaml:"else,omitempty"`

    // aggregator
    Op      string                 `yaml:"op,omitempty"`      // sum|avg|merge|reduce
    Over    []string               `yaml:"over,omitempty"`    // step ids

    Timeout time.Duration          `yaml:"timeout,omitempty"`
    Raw     map[string]any         `yaml:"-"`                 // passthrough
}

type StepType string

const (
    StepHTTPGet     StepType = "http_get"
    StepSysStat     StepType = "sys_stat"
    StepLogRead     StepType = "log_read"
    StepScript      StepType = "script"
    StepConditional StepType = "conditional"
    StepAggregator  StepType = "aggregator"
)

type Output struct {
    StepID   string         `json:"step_id"`
    OK       bool           `json:"ok"`
    Data     map[string]any `json:"data"`
    Error    string         `json:"error,omitempty"`
    Duration time.Duration  `json:"duration"`
    StartedAt time.Time     `json:"started_at"`
}

type RunContext struct {
    Pipeline *Pipeline
    Steps    map[string]Output // id -> output
    RunID    string
}

type Executor interface {
    Execute(ctx context.Context, step Step, rc *RunContext) (Output, error)
}

type ExportConfig struct {
    LocalPort     int    `yaml:"local_port,omitempty"`
    CloudEndpoint string `yaml:"cloud_endpoint,omitempty"`
}
```

---

### 🔌 Contrato script externo (stdin/stdout JSON)

Shelby lanza `<runtime> <file>` con stdin JSON, espera stdout JSON. stderr = logs.

**Stdin (ShelbyRequest):**
```json
{
  "step_id": "enrich",
  "run_id": "r_01HXYZ...",
  "pipeline": "Monitor Salud Alpha",
  "input": {
    "latency": 123,
    "disk": 47.5
  },
  "context": {
    "steps": {
      "health": { "ok": true, "data": { "response_time": 123 } },
      "disk":   { "ok": true, "data": { "percentage_used": 47.5 } }
    }
  },
  "env": { "SHELBY_VERSION": "0.1.0" }
}
```

**Stdout (ShelbyResponse):**
```json
{
  "ok": true,
  "data": { "score": 72.4, "note": "healthy" },
  "error": null,
  "metrics": { "custom_ms": 12 }
}
```

Reglas:
* Exit 0 + stdout JSON válido = éxito.
* Exit != 0 o JSON inválido = fallo; stderr capturado en `Output.Error`.
* Timeout por step (`step.timeout`, default 30s). Shelby mata proceso con SIGTERM → SIGKILL.
* stdout debe ser **una sola línea JSON** (o bloque entre marcadores `<<<SHELBY_OUT` / `SHELBY_OUT>>>` si script loggea en stdout).
* stdin cerrado antes de que proceso arranque su lógica.

**SDKs sugeridos (thin):**
* Python: `shelby.read()` → dict, `shelby.write(data)`.
* Node: `shelby.read()` Promise, `shelby.write(data)`.
* Rust: `shelby::read::<T>()`, `shelby::write(&v)`.
