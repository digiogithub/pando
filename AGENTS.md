## Pando is an AI assistant designed to improve the workflow of software developers

Whis project based in golang, is a fork of a archived project called "openproject" (different to OpenProject software based in Nodejs) and continued by Charmbracelet Crush changing the licence. This fork continues with MIT license and is open to contributions.

The project is indexed with the latest code remembrance tools under the name "pando." You also have other indexed projects that will be used as a basis for improvements.

## Get the most performance using the tools

You have remembrance tools for storing and retrieving context and information about the project, its structure, and previous decisions. Use these tools to keep track of the project's history and to make informed decisions based on past experiences. Always check the knowledge base (kb) for relevant information before making decisions or implementing new features. Use the code remembrance tools to monitor changes and updates in the project, ensuring that you are always working with the most up-to-date information. Use the tools of code of remembrances to search symbols, functions, and hybrid search (semantic search) across the codebase and related projects that pando reimplements or is based on.

For the remembrance tools, always use "pando" as the user_id or project_id to store and retrieve relevant information for this project.

Parallelize tasks when possible, but always ensure that you are not losing context or important information. Use the tools of code of remembrances to keep track of the different tasks and their progress, and to ensure that you are not missing any important details. Use the tools of spawn_agent and the rest of mesnada tools to delegate tasks when needed, but always ensure that you are providing clear instructions and context to the agents you are delegating to. When use the engine "claude", use the model "sonnet" for programming tasks and the model "opus" for planning and very harder tasks that require more reasoning and understanding of the context. Use preferently the engine "copilot" for programming tasks with the model "gpt-5.4" and "gpt-5-mini" for translations and very simple tasks.

### Development Workflow

- **Language**: Use always english for code, comments, and documentation.
- **Context Awareness**: Always be aware of the current context of the project. Use always the tools of kb and code of remembrances for retrieving relevant information about the project, its structure, and previous decisions. Activate the project monitoring with the tools of code of remembrances to keep track of changes and updates.
- **Planning**: If you are unsure about if you are following a plan, check with `last_to_remember` to see the current plan and confirm with the user if needed, before proceeding. If you don't have a plan, create one, split into phases, save them each fase using `save_fact` and save a summary of the plan using `to_remember` and save the plan in the kb of remembrances.
- **Implementation**: Write code in small, testable increments. After each increment, run tests to ensure functionality.
- **Code Style**: Follow Go best practices and project-specific coding standards
- **Testing**: Create tests in `tests/` folder (Python files, not in root)
- **Verified commands**:
  - Agent/API targeted tests: `go test ./internal/llm/agent ./internal/api`
- **Documentation**: Update documentation as needed, especially if you are adding new features or making significant changes. Use always for documentation the tools of kb of remembrances to save any relevant information about the project, the changes you are making, and the reasons behind those changes.

### External Research

- Use web search (google/brave/perplexity) for additional information when needed
- Use Context7 for API documentation and library usage patterns
