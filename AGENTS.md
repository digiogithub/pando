## Pando is an AI assistant designed to improve the workflow of software developers

Whis project based in golang, is a fork of a archived project called "openproject" (different to OpenProject software based in Nodejs) and continued by Charmbracelet Crush changing the licence. This fork continues with MIT license and is open to contributions.

The project is indexed with the latest code remembrance tools under the name "pando." You also have other indexed projects that will be used as a basis for improvements.

For the remembrance tools, always use "pando" as the user_id or project_id to store and retrieve relevant information for this project.

### Development Workflow

- **Language**: Use always english for code, comments, and documentation.
- **Context Awareness**: Always be aware of the current context of the project. Use always the tools of kb and code of remembrances for retrieving relevant information about the project, its structure, and previous decisions.
- **Planning**: If you are unsure about if you are following a plan, check with `last_to_remember` to see the current plan and confirm with the user if needed, before proceeding. If you don't have a plan, create one, split into phases, save them each fase using `save_fact` and save a summary of the plan using `to_remember`.
- **Implementation**: Write code in small, testable increments. After each increment, run tests to ensure functionality.
- **Code Style**: Follow Go best practices and project-specific coding standards
- **Testing**: Create tests in `tests/` folder (Python files, not in root)
- **Documentation**: Update documentation as needed, especially if you are adding new features or making significant changes. Use always for documentation the tools of kb of remembrances to save any relevant information about the project, the changes you are making, and the reasons behind those changes.

### External Research

- Use web search (google/brave/perplexity) for additional information when needed
- Use Context7 for API documentation and library usage patterns
