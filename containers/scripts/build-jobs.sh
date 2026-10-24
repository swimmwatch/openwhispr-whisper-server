#!/bin/bash
set -euo pipefail

if [[ -n ${BUILD_JOBS:-} ]]; then
    if [[ ! $BUILD_JOBS =~ ^[1-9][0-9]*$ ]]; then
        echo 'BUILD_JOBS must be a positive integer' >&2
        exit 2
    fi
    printf '%s\n' "$BUILD_JOBS"
    exit
fi

: "${BUILD_MEMORY_RESERVE_KIB:?Build memory reserve is required}"
: "${BUILD_MEMORY_PER_JOB_KIB:?Per-job memory budget is required}"
if [[ ! $BUILD_MEMORY_RESERVE_KIB =~ ^(0|[1-9][0-9]*)$ ]] ||
    [[ ! $BUILD_MEMORY_PER_JOB_KIB =~ ^[1-9][0-9]*$ ]]; then
    echo 'Build memory budgets must be nonnegative reserve and positive per-job integers' >&2
    exit 2
fi

memory_kib=$(awk '/MemAvailable:/ {print $2}' /proc/meminfo)
if [[ -r /sys/fs/cgroup/memory.max ]]; then
    limit=$(cat /sys/fs/cgroup/memory.max)
    current=$(cat /sys/fs/cgroup/memory.current)
    if [[ $limit != max ]]; then
        remaining_kib=$(( (limit - current) / 1024 ))
        if (( remaining_kib < memory_kib )); then
            memory_kib=$remaining_kib
        fi
    fi
fi

memory_jobs=$(( (memory_kib - BUILD_MEMORY_RESERVE_KIB) / BUILD_MEMORY_PER_JOB_KIB ))
if (( memory_jobs < 1 )); then
    memory_jobs=1
fi

jobs=$(nproc)
if [[ -r /sys/fs/cgroup/cpu.max ]]; then
    read -r quota period < /sys/fs/cgroup/cpu.max
    if [[ $quota != max ]]; then
        quota_jobs=$(( (quota + period - 1) / period ))
        if (( jobs > quota_jobs )); then
            jobs=$quota_jobs
        fi
    fi
fi
if (( jobs > memory_jobs )); then
    jobs=$memory_jobs
fi
printf '%s\n' "$jobs"
