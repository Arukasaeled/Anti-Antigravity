# Domain Docs

采用 single-context：根目录 CONTEXT.md，决策放在 docs/adr/。

探索前读取 CONTEXT.md 和相关 ADR。
若以后出现 CONTEXT-MAP.md，先读映射，再读相关 context。
这些文档不存在时继续工作，不提前创建空文档。
domain-modeling 在术语或决策实际明确后按需创建。

使用 CONTEXT.md 的术语；概念缺失时记录缺口，避免擅造同义词。
若提议与 ADR 冲突，明确指出 ADR 编号及重新讨论的理由。

现有架构和约定另见 docs/ARCHITECTURE.md、docs/DEVELOPMENT.md。
用户当前指令优先于文档中的测试、构建或验证要求。
