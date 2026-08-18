I am looking to improve my local multi-agent "Debate and Arbitrate" pipeline on a machine with a single **16GB VRAM GPU**.

My anchor model is **Qwen3.5-9B** at **Q4_K_M / Q4_K_XL** quantization, which consumes roughly 6GB to 7GB of VRAM. I have about 9GB of VRAM headroom remaining to manage secondary models, system overhead, and the context KV cache.

Please build out a production-ready system architecture plan and implementation blueprint that I can use to build this pipeline. Ensure the plan addresses the following requirements:

#### 1. Hardware & VRAM Allocation Map

- Total VRAM budget: 16GB.
- Primary Anchor (Debater A): Qwen3.5-9B Q4_K_XL (~7GB).
- Propose two specific VRAM topologies:
  - **Topology 1 (Concurrent):** Keeping all models resident in VRAM simultaneously without offloading or crashes. Suggest complementary 3B-class or 1.5B-class companion models (e.g., Llama-3.2-3B, Qwen2.5-3B, Phi-4-mini) and calculate strict KV cache / context window limits (e.g., 4K or 8K) to prevent out-of-memory (OOM) errors.
  - **Topology 2 (Sequential Swapping):** Dynamically loading/unloading models into VRAM using an orchestration tool to allow larger models (e.g., another 7B/9B model) to trade places on the GPU. I read that the Phi-4 models provide Excellent at precise logical reasoning, fact-checking, and structured breakdown, making it a strong structural arbiter. Qwen3.5-3B (Q5_K_M or Q8_0): Takes ~2.5GB to 3.5GB VRAM. Fast, shares the exact same tokenizer and family logic as your 9B model, making it a great lightweight debater or initial critic or Llama-3.2-3B-Instruct (Q4_K_M): Takes ~2GB VRAM. Offers an entirely different training paradigm and style to contrast cleanly against your Qwen 9B debater.

#### 2. Model Roles & Selection Matrix

Define the optimal model pairings and quantizations for:

- **Debater A:** (Anchor: Qwen3.5-9B Q4)
- **Debater B:** (Contrarian/Alternative perspective)
- **Arbiter:** (Synthesis, logic, fact-checking, final output generation)

#### 3. Agentic Workflow Logic

Outline the explicit iterative prompt-chaining loop:

1. Input Prompt $\rightarrow$ Debater A generates initial draft.
2. Draft $\rightarrow$ Debater B reviews, finds flaws, and argues an alternative angle.
3. Debater A Draft + Debater B Critique $\rightarrow$ Arbiter synthesizes, fixes factual inconsistencies, and produces the finalized asset.

#### 4. Aegis Framework Implementation

Provide a clean blueprint using a local execution engine (such as **Ollama API** or **LangChain / CrewAI** using OpenAI-compatible local endpoints like llama.cpp).

- **If Concurrent:** Include specific `num_ctx` or configuration parameters to lock down the memory footprint.
- **If Sequential:** Show how to explicitly leverage Ollama's `/api/generate` or model-loading lifecycle behavior (sending a `keep_alive: 0` or unloading models) to clear VRAM between agent rounds.

Please provide a highly structured, step-by-step breakdown of the architecture, followed by the complete, documented Python code script.
