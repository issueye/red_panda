// Split from SettingsPanel.jsx (checklist R7c)
import { Blocks, Plus, RefreshCw } from 'lucide-react';
import { emptySkillDraft, ModuleHeader, ManagerItem } from './shared.jsx';
import { Button, IconButton } from '../ui/button.jsx';
import { EmptyState, ErrorMessage } from '../ui/feedback.jsx';
import { Field } from '../ui/field.jsx';
import { Badge } from '../ui/badge.jsx';


export function SkillsTab({
  visibleSkills,
  skillsLoading,
  skillsError,
  skillDraft,
  skillSaving,
  skillError,
  workspaceRoot,
  setSkillDraft,
  setSkillError,
  saveSkill,
  selectSkill,
  deleteSkill,
  refreshSkills,
}) {
  return (
    <>
          <ModuleHeader badge={`${visibleSkills.length} 个技能`} title="技能管理">
            <IconButton
              disabled={skillsLoading || skillSaving || !workspaceRoot}
              label="刷新技能"
              onClick={refreshSkills}
            >
              <RefreshCw size={15} />
            </IconButton>
            <Button
              disabled={!workspaceRoot || skillSaving}
              icon={<Plus size={15} />}
              onClick={() => {
                setSkillError?.('');
                setSkillDraft({ ...emptySkillDraft });
              }}
              variant="soft"
            >
              新建技能
            </Button>
          </ModuleHeader>
          {skillsError || skillError ? (
            <ErrorMessage className="settings-error">{skillError || skillsError}</ErrorMessage>
          ) : null}
          <div className="settings-manager-grid">
            <div className="settings-manager-list" data-testid="settings-skill-list">
              {visibleSkills.length === 0 ? (
                <EmptyState title={skillsLoading ? '正在加载技能' : '暂无托管技能'}>
                  {workspaceRoot
                    ? '创建 skill 后会写入当前工作区 .codex/skills。'
                    : '打开工作区后可管理托管技能。'}
                </EmptyState>
              ) : visibleSkills.map((skill) => (
                <ManagerItem
                  active={skillDraft?.name === skill.name && !skillDraft?.isNew}
                  disabled={skillSaving}
                  enabled
                  icon={Blocks}
                  key={skill.name}
                  meta={skill.path || skill.description}
                  name={skill.name}
                  onDelete={() => deleteSkill(skill)}
                  onSelect={() => selectSkill(skill)}
                  onToggle={() => {}}
                />
              ))}
            </div>
            <div className="settings-editor-panel settings-collection-editor">
              {skillDraft ? (
                <>
                  <div className="settings-editor-title">
                    <strong>{skillDraft.isNew ? '新建技能' : '编辑技能'}</strong>
                    <Badge>工作区</Badge>
                  </div>
                  <Field className="settings-row" label="名称">
                    <input
                      disabled={!skillDraft.isNew || skillSaving}
                      onChange={(event) => setSkillDraft((current) => ({ ...current, name: event.target.value }))}
                      placeholder="code-review"
                      type="text"
                      value={skillDraft.name}
                    />
                  </Field>
                  {skillDraft.path ? (
                    <Field className="settings-row" label="路径">
                      <input disabled readOnly type="text" value={skillDraft.path} />
                    </Field>
                  ) : null}
                  <Field className="settings-row" label="描述">
                    <textarea
                      disabled={skillSaving}
                      onChange={(event) => setSkillDraft((current) => ({ ...current, description: event.target.value }))}
                      placeholder="用于选择该技能的简短说明"
                      rows={3}
                      value={skillDraft.description}
                    />
                  </Field>
                  <Field className="settings-row" label="指令">
                    <textarea
                      disabled={skillSaving}
                      onChange={(event) => setSkillDraft((current) => ({ ...current, instructions: event.target.value }))}
                      placeholder="完整 Markdown 指令"
                      rows={8}
                      value={skillDraft.instructions}
                    />
                  </Field>
                  <div className="settings-editor-actions">
                    <Button onClick={() => setSkillDraft(null)} variant="ghost">取消</Button>
                    <Button
                      disabled={
                        !skillDraft.name.trim()
                        || !skillDraft.description.trim()
                        || !skillDraft.instructions.trim()
                        || !workspaceRoot
                      }
                      loading={skillSaving}
                      onClick={saveSkill}
                    >
                      保存技能
                    </Button>
                  </div>
                </>
              ) : (
                <EmptyState title="选择技能">从左侧选择技能，或新建一个托管技能。</EmptyState>
              )}
            </div>
          </div>
        </>
  );
}
