# Remote Assistance

> This document describes a differentiated **enhancement direction** of Servify, not a delivery prerequisite of the first version of the product.
>
> The V1 product goal has converged on [Web standalone-site intelligent customer service](./v1-product-scope.md): `Web 接入 -> AI 首答 -> 人工接管 -> 转接协作 -> 工单全流程`. Remote assistance keeps its architecture and real-time capability foundation, but is not the center of V1 acceptance. This document is used to explain its product intent, current foundation, and subsequent evolution boundary.

Remote assistance is one of the differentiated directions worth continuing to invest in for Servify.

Many customer service systems can do:

- Online chat
- FAQ or bot Q&A
- Ticket flow

But in real service delivery, many problems cannot be solved just by "replying with a paragraph of text"; they also need:

- Walking the customer through an operation step by step
- Locating the problem together with the customer
- Continuing to collaborate in the same context

Servify understands this kind of capability as a product-level `远程协助` (remote assistance), rather than treating it only as an underlying real-time communication capability.

## One-line Definition

In Servify, remote assistance means:

- Within the same session, the agent upgrades the service from "explaining in text" to "real-time guidance, joint troubleshooting, and continued takeover"
- After the assistance ends, the context can still stay within human collaboration, transfer, and the full ticket workflow

It emphasizes the continuous service chain, rather than emphasizing a specific protocol term.

## What Problems Remote Assistance Solves

Typical scenarios include:

- The customer cannot operate a certain feature and needs to be guided through it
- The customer cannot describe the problem clearly and needs to confirm while looking
- After-sales support or technical support needs to help with troubleshooting
- In internal service desk scenarios, a colleague needs help completing a configuration or locating a fault

If the system can only chat, the agent flow stays at "send a message to explain"; with remote assistance, agents can enter a state much closer to the real service process.

## Position in Servify

Servify's V1 product main chain is:

`Web 接入 -> AI 首答 -> 人工接管 -> 转接协作 -> 工单全流程`

Remote assistance is not in this V1 main chain; it hangs as an optional upgrade direction after `人工接管` (human handover). This means:

- V1 first guarantees that website access, AI first answer, human handover, and the full ticket workflow hold
- When text communication is sufficient, staying at AI or human replies is fine
- When the problem needs a demonstration, guidance, or joint troubleshooting, it can be upgraded to remote assistance in the future
- The current repository has kept the real-time capability foundation such as WebSocket / WebRTC, but does not commit to having delivered a complete remote-control product

In other words, remote assistance is an enhancement layer after `人工接管` (human handover), not a capability that V1 must accept.

## Current Product Expression

At the product level, remote assistance should be expressed as:

- Helping customers complete operations faster
- Letting agents move from "explaining the problem" to "solving it along with them"
- Keeping complex problems inside the same service chain, instead of jumping to another tool and breaking the context

At the technical implementation level, Servify currently already has the real-time capability foundation related to remote assistance, including WebSocket / WebRTC related chains, but the documentation and product wording should not put the emphasis only on protocol terms.

## What Already Exists in the Current Repository

The foundation that can currently be clearly expressed externally includes:

- Web sessions, messages, and the real-time reception chain already exist
- AI first answer, human handover, transfer collaboration, and ticket flow are all included in the same product main chain
- WebSocket / WebRTC related runtime, statistics, and connection views have already appeared in code and acceptance
- The documentation and README have clearly positioned remote assistance as an enhancement direction after V1, rather than a mandatory node in the V1 main chain `AI 首答 -> 人工接管 -> 工单全流程`

What should not be promised ahead of schedule includes:

- A complete co-browsing / screen-sharing product interface has been delivered
- A dedicated remote-assistance workspace for the agent console has been completed
- A standardized end-to-end demo script and product acceptance convention has been completed

These belong to the scope that `Gap C` continues to close, not to facts the current repository has already delivered.

The current entry points and gap inventory are in:

- [Remote Assistance Current State Inventory](./remote-assistance-current-state.md)
- [Remote Assistance Minimum Deliverable Chain](./remote-assistance-mvp.md)

## Relationship with AI, Humans, and Tickets

Remote assistance works together with the other core capabilities:

- AI can do the first answer and clarification first, reducing the cost of human intervention
- Humans take over when needed and initiate remote assistance
- After assistance, the session can still be transferred to a more suitable handler
- Problems that cannot be closed immediately can be settled into tickets and continue to advance

This way, the customer service system is not just a "chat tool + ticket tool" but a continuous service product.

## Current-Stage Judgment

Servify already has direction and foundation on this matter, but in the V1 phase it should actively converge the scope, avoiding letting remote assistance overshadow the website customer service main chain:

- V1 does not make remote assistance a user-visible delivery commitment; first guarantee that AI first answer, human handover, and the full ticket workflow are stable
- Keep the real-time capability foundation and the entry point for later expansion, rather than spending maturity ahead of time
- In the documentation, official website, and product narrative, state it truthfully as an "enhancement direction", not a delivered core capability

Later, after the V1 main chain stabilizes, continue to fill in three things:

- What directly usable remote assistance entry points actually exist in the agent console and the server today
- How the minimum deliverable chain should upgrade from a Web session to human assistance and then into a ticket
- One reusable demo and acceptance convention, rather than only verifying that the underlying WebRTC / WebSocket works
