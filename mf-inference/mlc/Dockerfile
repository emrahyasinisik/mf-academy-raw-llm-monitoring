# MLC-LLM serving image.
#
# Base image and wheel tag must agree on the CUDA minor version. The host
# driver may be newer — NVIDIA drivers are backward compatible with older CUDA
# runtimes, so a 13.x driver runs a 12.2 runtime fine. It is the *wheel* that is
# picky, not the driver.
FROM nvidia/cuda:12.2.2-runtime-ubuntu22.04

ARG CUDA_TAG=cu122

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

# git-lfs is needed because HF:// model repos store weights as LFS objects.
RUN apt-get update && apt-get install -y --no-install-recommends \
        python3 python3-pip git git-lfs curl ca-certificates \
    && git lfs install --system \
    && rm -rf /var/lib/apt/lists/*

# MLC ships nightly wheels from its own index rather than PyPI. Both packages
# are required: mlc-ai is the TVM runtime, mlc-llm is the engine on top of it.
RUN pip3 install --pre -U -f https://mlc.ai/wheels \
        "mlc-llm-nightly-${CUDA_TAG}" \
        "mlc-ai-nightly-${CUDA_TAG}"

# Weights land here; docker-compose mounts a named volume so they are downloaded
# once rather than on every rebuild.
ENV HF_HOME=/cache/huggingface \
    MLC_LLM_HOME=/cache/mlc
RUN mkdir -p /cache

EXPOSE 8000

# The concrete `mlc_llm serve` invocation (model, device, mode) lives in
# docker-compose.yml so the model can be changed without rebuilding the image.
CMD ["mlc_llm", "serve", "--help"]
