-- Focused examples for prompt customization hooks.

function hook_template_section(ctx)
    if ctx.section_name == "base/workflow" then
        ctx.section_content = ctx.section_content .. "\n\n- Always inspect relevant files completely before editing."
    end
    return ctx
end

function hook_capability_check(ctx)
    -- Example: force-disable browser/web capabilities for a conservative agent.
    if ctx.agent_name == "auditor" and (ctx.capability == "web_search" or ctx.capability == "orchestration") then
        ctx.available = false
    end
    return ctx
end

function hook_provider_select(ctx)
    -- Example: override provider template selection.
    if ctx.provider == "openai" and ctx.model_family == "o-series" then
        ctx.provider_template = "providers/openai/o-series"
    end
    return ctx
end

function hook_prompt_compose(ctx)
    table.insert(ctx.sections, 1, {
        name = "lua/prelude",
        content = "## Local Prompt Prelude\n- Respect repository conventions.\n- Make minimal changes."
    })
    return ctx
end

function hook_system_prompt(ctx)
    local git = pando_get_git_status()
    if git and git.is_repo then
        ctx.system_prompt = ctx.system_prompt .. "\n\nCurrent repository: " .. (git.working_dir or "unknown")
    end
    return ctx
end
