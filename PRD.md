# Mnemos — Product Requirements Document (PRD)

# 1\. Overview

Mnemos transforms arbitrary inputs into structured, evolving, evidence-backed knowledge. It enables users to understand what is true and why.

# 2\. Problem Statement

## 2.1 Fragmented Knowledge

Information is spread across disparate sources such as documents, chats, logs, and spreadsheets, leading to a lack of unified understanding.

## 2.2 Loss of Context

Critical reasoning, assumptions, and historical context are often lost when decisions are made.

## 2.3 Conflicting Information

Different sources often contradict each other, creating uncertainty and making reconciliation difficult.

## 2.4 AI Unreliability

Current AI systems frequently hallucinate, lack persistent memory, and cannot explain their reasoning.

# 3\. User Personas

## 3.1 Primary Users

1. AI Engineers  
2. Knowledge Workers  
3. Product Teams

## 3.2 Secondary Users

4. Founders / Executives  
5. Analysts / Researchers

# 4\. Core Use Case (CRITICAL)

*Scenario: "What happened to our investment?"*  
**Inputs:** Meeting transcripts, documents, and metrics spreadsheets.  
**Output:** A timeline of events, key decisions, claims (what was believed), contradictions (what changed), and supporting evidence.

# 5\. Product Vision

Mnemos turns any input into structured, evolving knowledge that can be queried and trusted over time.

# 6\. Desired Outcomes

## 6.1 Primary Outcome

Users can reliably answer: "What is true and why?"

## 6.2 Secondary Outcomes

6. Understand how knowledge evolved  
7. Identify contradictions  
8. Trace decisions back to evidence  
9. Build trust in system outputs

# 7\. Solution Architecture

**Core Pipeline:** Inputs → Events → Claims → Relationships → Truth

## 7.1 Pipeline Stages

10. **Inputs:** Raw data (docs, logs, etc.)  
11. **Events:** Normalized units of information  
12. **Claims:** Extracted knowledge and facts  
13. **Relationships:** Detection of support or contradiction  
14. **Truth:** Evolving understanding over time

# 8\. Core Capabilities

## 8.1 Input Ingestion

Support for files (TXT, MD, JSON, CSV), folders, and raw text input.

## 8.2 Parsing

Normalization of various inputs into discrete events.

## 8.3 Event Store

An append-only source of truth for all processed information.

## 8.4 Claim Extraction

Automated extraction of facts, hypotheses, and decisions.

## 8.5 Relationship Detection

Identification of supporting and contradictory claims.

## 8.6 Query Interface

Natural language interface returning answers with claims, contradictions, and timelines.

# 9\. MVP Scope

## 9.1 Included

15. Input ingestion (files \+ text)  
16. Parsing (TXT, JSON, CSV)  
17. Event store and claim extraction  
18. Relationship detection  
19. CLI-based query interface

## 9.2 Excluded

20. Graphical User Interface (UI)  
21. Real-time ingestion and governance  
22. Multi-modal inputs (images, slides)

# 10\. Success Metrics

23. Time-to-value \< 5 minutes  
24. ≥70% of queries return structured claims  
25. Contradictions surfaced in all complex queries  
26. Users prefer output over standard RAG systems

# 11\. Product Bets

27. **Inputs → Knowledge:** Users want structured understanding from arbitrary inputs.

Users want to provide any input and get structured understanding.

28. **Claims vs Docs:** Users prefer interacting with claims over raw documents.

Users prefer claims over raw documents.

29. **Conflict vs Trust:** Exposing conflict increases overall system trust.

Users trust systems that expose conflict.

30. **Evidence vs RAG:** Grounded, evidence-backed answers outperform RAG.

Users prefer grounded answers.

# 12\. Risks and Constraints

## 12.1 Risks

31. Low-quality claim extraction or false contradictions  
32. Poor query relevance or perceived lack of differentiation from RAG

## 12.2 Constraints

33. Local-first architecture with simple setup  
34. Minimal dependencies and no heavy infrastructure

# 13\. Non-Goals

35. Workflow automation and agent orchestration  
36. Collaboration features and real-time streaming

# 14\. Open Questions

37. What is the threshold for acceptable claim accuracy?  
38. How should contradiction detection be tuned?  
39. When is the appropriate time to introduce governance?

# 15\. Definition of Success

Mnemos is successful when users can answer complex questions across messy inputs, answers are trusted due to evidence, and the system feels fundamentally superior to search or RAG.  
