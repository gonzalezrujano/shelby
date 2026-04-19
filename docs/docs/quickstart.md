---
sidebar_position: 3
---

# Quickstart

Here is how to create a pipeline that checks disk usage and schedules it with Shelby.

### 1. Write a pipeline

Create a file named `disk.yaml`:

```yaml
name: "Disk Watch"
interval: 30s
steps:
  - id: df
    type: shell
    command: "df -Pk /"
    parse:
      engine: textfsm
      template: ./examples/templates/df.textfsm
      types: { BLOCKS: int, USED: int, AVAIL: int, PCT: int }

  - id: max_pct
    type: aggregator
    op: max
    over: ["${steps.df.output.records}"]
    field: PCT

output:
  mount:   ${steps.df.output.records.0.MOUNT}
  pct:     ${steps.max_pct.output.value}
```

### 2. Run it ad-hoc

You can run pipelines instantly without saving them in the scheduling context:

```bash
shelby run disk.yaml
```

### 3. Register & Schedule

To run it continuously in the background, register it into Shelby's system:

```bash
shelby add disk.yaml
shelby list
shelby serve          # Spawns web UI at http://localhost:8080
```
