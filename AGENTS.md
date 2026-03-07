## Pando is an AI assistant designed to improve the workflow of software developers

Whis project based in golang, is a fork of a archived project called "openproject" (different to OpenProject software based in Nodejs) and continued by Charmbracelet Crush changing the licence. This fork continues with MIT license and is open to contributions.

El proyecto está indexado con last tools de code de remembrances con nombre pando. También tienes otros proyectos indexados que se usarán como base de mejoras.

Para las tools de remembrances siempre usa como user_id o project_id "pando" para guardar y recuperar información relevante a este proyecto.

### Development Workflow

- **Context Awareness**: Always be aware of the current context of the project. Use always the tools of kb and code of remembrances for retrieving relevant information about the project, its structure, and previous decisions.
- **Planning**: If you are unsure about if you are following a plan, check with `last_to_remember` to see the current plan and confirm with the user if needed, before proceeding. If you don't have a plan, create one, split into phases, save them each fase using `save_fact` and save a summary of the plan using `to_remember`.
- **Implementation**: Write code in small, testable increments. After each increment, run tests to ensure functionality.
- **Code Style**: Follow Go best practices and project-specific coding standards
- **Testing**: Create tests in `tests/` folder (Python files, not in root)
- **Documentation**: Update documentation as needed, especially if you are adding new features or making significant changes. Use always for documentation the tools of kb of remembrances to save any relevant information about the project, the changes you are making, and the reasons behind those changes.

### External Research

- Use web search (google/brave/perplexity) for additional information when needed
- Use Context7 for API documentation and library usage patterns
