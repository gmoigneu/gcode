package compaction

// SummarizationSystemPrompt is the system message used for every
// compaction LLM call.
const SummarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant and produce a structured summary. Do NOT continue the conversation. Do NOT add commentary. Only output the structured summary.`

// SummarizationPrompt is used for a fresh compaction when no previous
// summary exists.
const SummarizationPrompt = `Summarize this conversation as a structured checkpoint. Use this exact format:

## Goal
What is the user trying to achieve?

## Constraints & Preferences
Any stated requirements, preferences, or boundaries.

## Progress
- Done: What has been completed.
- In Progress: What is currently being worked on.
- Blocked: What is stuck and why.

## Key Decisions
Important choices made during the conversation and their rationale.

## Next Steps
What should be done next.

## Critical Context
Anything essential for continuing the work that doesn't fit above (variable names, file paths, error messages, configuration details).`

// UpdateSummarizationPrompt is used for iterative compaction. The %s is
// replaced with the previous summary before the new conversation segment
// is appended.
const UpdateSummarizationPrompt = `You have a previous summary of an ongoing conversation and a new segment of that conversation. Update the summary to incorporate the new information.

Rules:
- Merge new information into the existing structure. Do not duplicate.
- Update the Progress section: move completed items from "In Progress" to "Done", add new items.
- Update Key Decisions with any new decisions made.
- Update Next Steps based on current state.
- Keep the same structured format.

Previous summary:
%s

New conversation segment follows.`

// TurnPrefixPrompt is used when a cut splits a turn. The messages from
// the turn start to the cut point are summarised separately.
const TurnPrefixPrompt = `Summarize the beginning of this conversation turn. This is a partial turn that was split during context compaction. Focus on:

## Original Request
What did the user ask for in this turn?

## Early Progress
What was accomplished in the portion being summarized?

## Context for Suffix
What context does the remaining (unsummarized) portion of this turn need to make sense?`

// BranchSummaryPreamble is prepended to a branch summary so the main
// context knows the summary describes an abandoned branch.
const BranchSummaryPreamble = `The user explored a different conversation branch before returning here.`

// BranchSummaryPrompt is used for branch summarisation.
const BranchSummaryPrompt = `Summarize this conversation branch that the user explored before switching back. Use this format:

## Goal
What was the user trying to achieve on this branch?

## Constraints & Preferences
Any stated requirements or preferences.

## Progress
- Done: What was completed.
- In Progress: What was being worked on.
- Blocked: What was stuck.

## Key Decisions
Important choices made on this branch.

## Next Steps
What the user intended to do next (before switching branches).

## Critical Context
Anything from this branch that may be relevant to the branch the user returned to.`
