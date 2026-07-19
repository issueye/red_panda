import { Check, ChevronDown, ChevronLeft, ChevronRight } from 'lucide-react';
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { classNames } from '../../lib/format.js';
import { providerModelFor, reasoningEffortOptions } from '../../lib/providerProfiles.js';

function modelsFor(profile) {
  if (!profile) return [];
  return profile.models?.length
    ? profile.models
    : [providerModelFor(profile)].filter(Boolean);
}

export function ComposerModelMenu({
  disabled = false,
  model = '',
  onProviderProfileChange,
  onReasoningEffortChange,
  providerProfileId = '',
  providerProfiles = [],
  reasoningEffort = '',
}) {
  const rootRef = useRef(null);
  const triggerRef = useRef(null);
  const panelRef = useRef(null);
  const childPanelRef = useRef(null);
  const [open, setOpen] = useState(false);
  const [submenu, setSubmenu] = useState('');
  const [coords, setCoords] = useState(null);

  const activeProfiles = useMemo(() => {
    const profiles = providerProfiles.filter((item) => item.active !== false);
    const selected = providerProfiles.find((item) => item.id === providerProfileId);
    if (selected && !profiles.some((item) => item.id === selected.id)) profiles.push(selected);
    return profiles;
  }, [providerProfileId, providerProfiles]);

  const modelGroups = useMemo(() => activeProfiles.map((profile) => ({
    id: profile.id,
    name: profile.name,
    models: modelsFor(profile),
  })).filter((group) => group.models.length > 0), [activeProfiles]);

  const selectedProfile = providerProfiles.find((item) => item.id === providerProfileId) || null;
  const selectedModel = modelsFor(selectedProfile).find((item) => item.model === model);
  const modelLabel = selectedModel?.label || selectedModel?.model || model || '默认模型';
  const reasoningLabel = reasoningEffortOptions.find((item) => item.value === reasoningEffort)?.label || '默认';

  function updatePosition() {
    const trigger = triggerRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const viewportPadding = 8;
    const panelWidth = 226;
    const childWidth = 270;
    const gap = 6;
    const left = Math.min(
      Math.max(viewportPadding, rect.right - panelWidth),
      window.innerWidth - panelWidth - viewportPadding,
    );
    const childFitsLeft = left - childWidth - gap >= viewportPadding;
    const childFitsRight = left + panelWidth + gap + childWidth <= window.innerWidth - viewportPadding;
    const childLeft = childFitsLeft
      ? left - childWidth - gap
      : childFitsRight
        ? left + panelWidth + gap
        : Math.max(viewportPadding, Math.min(left, window.innerWidth - childWidth - viewportPadding));
    setCoords({
      bottom: window.innerHeight - rect.top + 6,
      childLeft,
      childWidth,
      left,
      panelWidth,
    });
  }

  useLayoutEffect(() => {
    if (!open) {
      setCoords(null);
      return undefined;
    }
    updatePosition();
    const reposition = () => updatePosition();
    window.addEventListener('resize', reposition);
    window.addEventListener('scroll', reposition, true);
    return () => {
      window.removeEventListener('resize', reposition);
      window.removeEventListener('scroll', reposition, true);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return undefined;
    const closeOnOutsidePointer = (event) => {
      const target = event.target;
      if (rootRef.current?.contains(target)
        || panelRef.current?.contains(target)
        || childPanelRef.current?.contains(target)) return;
      setOpen(false);
      setSubmenu('');
    };
    const handleKeyDown = (event) => {
      if (event.key === 'Escape' && submenu) {
        event.preventDefault();
        setSubmenu('');
      } else if (event.key === 'Escape') {
        event.preventDefault();
        setOpen(false);
        triggerRef.current?.focus();
      } else if (event.key === 'ArrowLeft' && submenu) {
        event.preventDefault();
        setSubmenu('');
      }
    };
    document.addEventListener('pointerdown', closeOnOutsidePointer);
    document.addEventListener('keydown', handleKeyDown, true);
    return () => {
      document.removeEventListener('pointerdown', closeOnOutsidePointer);
      document.removeEventListener('keydown', handleKeyDown, true);
    };
  }, [open, submenu]);

  function chooseModel(nextProviderId, nextModel) {
    onProviderProfileChange?.(nextProviderId, nextModel);
    setOpen(false);
    setSubmenu('');
    triggerRef.current?.focus();
  }

  function chooseReasoning(nextEffort) {
    onReasoningEffortChange?.(nextEffort);
    setOpen(false);
    setSubmenu('');
    triggerRef.current?.focus();
  }

  const panels = open && coords ? createPortal(
    <>
      <div
        aria-label="模型设置"
        className="composer-model-panel"
        data-testid="composer-model-panel"
        ref={panelRef}
        role="menu"
        style={{
          bottom: `${coords.bottom}px`,
          left: `${coords.left}px`,
          width: `${coords.panelWidth}px`,
        }}
      >
        <button
          className={classNames('composer-model-panel-row', submenu === 'models' && 'is-active')}
          data-testid="composer-model-menu-models"
          onClick={() => setSubmenu('models')}
          role="menuitem"
          type="button"
        >
          <span>模型</span>
          <span className="composer-model-panel-value">{modelLabel}</span>
          <ChevronRight aria-hidden="true" size={15} />
        </button>
        <button
          className={classNames('composer-model-panel-row', submenu === 'reasoning' && 'is-active')}
          data-testid="composer-model-menu-reasoning"
          onClick={() => setSubmenu('reasoning')}
          role="menuitem"
          type="button"
        >
          <span>推理强度</span>
          <span className="composer-model-panel-value">{reasoningLabel}</span>
          <ChevronRight aria-hidden="true" size={15} />
        </button>
      </div>

      {submenu ? (
        <div
          aria-label={submenu === 'models' ? '选择模型' : '选择推理强度'}
          className="composer-model-submenu"
          data-testid={`composer-model-${submenu}-submenu`}
          ref={childPanelRef}
          role="menu"
          style={{
            bottom: `${coords.bottom}px`,
            left: `${coords.childLeft}px`,
            width: `${coords.childWidth}px`,
          }}
        >
          <div className="composer-model-submenu-header">
            <button aria-label="返回模型设置" onClick={() => setSubmenu('')} type="button">
              <ChevronLeft aria-hidden="true" size={15} />
            </button>
            <span>{submenu === 'models' ? '模型' : '推理强度'}</span>
          </div>
          <div className="composer-model-submenu-scroll">
            {submenu === 'models' ? (
              <>
                <button
                  aria-checked={!providerProfileId}
                  className={classNames('composer-model-option', !providerProfileId && 'is-selected')}
                  onClick={() => chooseModel('', '')}
                  role="menuitemradio"
                  type="button"
                >
                  <span>默认模型</span>
                  {!providerProfileId ? <Check aria-hidden="true" size={14} /> : null}
                </button>
                {modelGroups.map((group) => (
                  <div className="composer-model-group" key={group.id}>
                    <div className="composer-model-group-label">{group.name}</div>
                    {group.models.map((item) => {
                      const selected = group.id === providerProfileId && item.model === model;
                      return (
                        <button
                          aria-checked={selected}
                          className={classNames('composer-model-option', selected && 'is-selected')}
                          key={`${group.id}:${item.model}`}
                          onClick={() => chooseModel(group.id, item.model)}
                          role="menuitemradio"
                          type="button"
                        >
                          <span>{item.label || item.model}</span>
                          {selected ? <Check aria-hidden="true" size={14} /> : null}
                        </button>
                      );
                    })}
                  </div>
                ))}
              </>
            ) : reasoningEffortOptions.map((item) => {
              const selected = item.value === reasoningEffort;
              return (
                <button
                  aria-checked={selected}
                  className={classNames('composer-model-option', selected && 'is-selected')}
                  key={item.value || 'default'}
                  onClick={() => chooseReasoning(item.value)}
                  role="menuitemradio"
                  type="button"
                >
                  <span>{item.label}</span>
                  {selected ? <Check aria-hidden="true" size={14} /> : null}
                </button>
              );
            })}
          </div>
        </div>
      ) : null}
    </>,
    document.body,
  ) : null;

  return (
    <div className={classNames('composer-model-menu', open && 'is-open')} ref={rootRef}>
      <button
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label="选择模型和推理强度"
        className="composer-model-menu-trigger"
        data-testid="composer-model-menu"
        disabled={disabled}
        onClick={() => {
          setOpen((current) => !current);
          setSubmenu('');
        }}
        ref={triggerRef}
        type="button"
      >
        <span>{modelLabel}</span>
        <span className="composer-model-menu-effort">{reasoningLabel}</span>
        <ChevronDown aria-hidden="true" size={14} />
      </button>
      {panels}
    </div>
  );
}
