# MLC-LLM serving image.
#
# Base image and wheel tag must agree on the CUDA minor version. The host
# driver may be newer — NVIDIA drivers are backward compatible with older CUDA
# runtimes, so a 13.x driver runs a 12.2 runtime fine. It is the *wheel* that is
# picky, not the driver.
#
# devel, not runtime: mlc_llm JIT-compiles CUDA kernels for the specific GPU
# architecture (sm_75 here) on first run, which needs nvcc. The runtime image
# only has the CUDA runtime libraries, not the compiler — serve fails with
# "RuntimeError: [Errno 2] No such file or directory: 'nvcc'" and Docker just
# keeps restarting the container since the failure looks like a crash, not a
# missing-binary error, to the `restart: unless-stopped` policy.
FROM nvidia/cuda:12.8.1-devel-ubuntu22.04

ARG CUDA_TAG=cu128

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

# git-lfs is needed because HF:// model repos store weights as LFS objects.
# libx11-6/libxext6 and libvulkan1 are runtime deps of the compiled TVM
# runtime (linked in even though we only use the CUDA backend, not X11/Vulkan).
# No python3-pip here: the MLC docs call for a conda-managed environment
# instead of system Python, specifically to avoid the apt/pip package
# manager fighting over things like setuptools (which we hit before this).
RUN apt-get update && apt-get install -y --no-install-recommends \
        git git-lfs curl ca-certificates \
        libx11-6 libxext6 libvulkan1 \
    && git lfs install --system \
    && rm -rf /var/lib/apt/lists/*

# Miniforge is the conda-forge community installer (no Anaconda ToS). python
# is pinned to 3.10, not left to the installer default: several of mlc-llm's
# transitive deps (torch, sentencepiece, the nvidia-cu13 wheels) only publish
# cp310 wheels, and a different interpreter forces a from-source build with
# no compiler toolchain in this image.
RUN curl -fsSL -o /tmp/miniforge.sh \
        https://github.com/conda-forge/miniforge/releases/latest/download/Miniforge3-Linux-x86_64.sh \
    && bash /tmp/miniforge.sh -b -p /opt/conda \
    && rm /tmp/miniforge.sh \
    && /opt/conda/bin/conda create -n mlc -y python=3.10 \
    && ln -s /opt/conda/envs/mlc/bin/python3 /usr/local/bin/python3 \
    && ln -s /opt/conda/envs/mlc/bin/pip3 /usr/local/bin/pip3

# MLC ships nightly wheels from its own index rather than PyPI. Both packages
# are required: mlc-ai is the TVM runtime, mlc-llm is the engine on top of it.
# mlc-llm also depends on ordinary PyPI packages (e.g. shortuuid), so we can't
# use --no-index here. Instead the >=0.15 floor is deliberate: MLC holds empty
# v0.1 placeholder packages under these same names on PyPI (to stop them being
# squatted), and a dropped/renamed CUDA tag on the real index would otherwise
# make pip fall back to that placeholder — "succeeding" with no mlc_llm binary.
# The floor makes that placeholder version-ineligible, forcing an honest error.
#
# CUDA_TAG must be one of the actively-published tags on that index — check
# https://llm.mlc.ai/docs/install/mlc_llm.html for the current set.
#
# Deliberately using the "mlc-llm-<tag>"/"mlc-ai-<tag>" packages, NOT
# "mlc-llm-nightly-<tag>"/"mlc-ai-nightly-<tag>": as of 2026-07-22 the nightly
# mlc-llm wheel is stuck at 0.20.dev162 across every CUDA tag while nightly
# mlc-ai has moved on per-tag (0.26.dev203 for cu128/cu130).
#
# apache-tvm-ffi is pinned to 0.1.11, not left to the resolver: --pre
# (required for the MLC wheels themselves) lets pip pick 0.1.13rc1, which
# drops/renames a C++ symbol every mlc-ai build we've tried was compiled
# against ("undefined symbol" on load). 0.1.12 is *also* broken, independent
# of anything MLC-specific — it introduced a duplicate __ffi_repr__
# registration bug for anything compiled against tvm-ffi <0.1.12, reported
# upstream as apache/tvm-ffi via tile-ai/tilelang#2367 and still open as of
# 2026-07-22. 0.1.11 is the last release before that break.
RUN pip3 install --pre -U -f https://mlc.ai/wheels \
        "mlc-llm-${CUDA_TAG}>=0.15" \
        "mlc-ai-${CUDA_TAG}>=0.15" \
        "apache-tvm-ffi==0.1.11"

# tvm.testing is imported unconditionally by tvm/__init__'s import chain
# (tvm -> s_tir -> meta_schedule -> rpc -> tvm.testing -> tvm_ffi.testing),
# even though we never run tests, and it hard-imports pytest. Not part of the
# main install so this stays its own layer and doesn't invalidate that cache.
RUN pip3 install pytest

# Weights land here; docker-compose mounts a named volume so they are downloaded
# once rather than on every rebuild.
ENV HF_HOME=/cache/huggingface \
    MLC_LLM_HOME=/cache/mlc
RUN mkdir -p /cache

EXPOSE 8000

# The concrete `mlc_llm serve` invocation (model, device, mode) lives in
# docker-compose.yml so the model can be changed without rebuilding the image.
CMD ["python3", "-m", "mlc_llm", "serve", "--help"]
