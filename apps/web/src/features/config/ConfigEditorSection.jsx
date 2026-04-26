export function ConfigEditorSection({ section }) {
  return (
    <section className="panel editor-panel config-editor-panel">
      <div className="panel-header editor-panel-header">
        <div>
          <p className="eyebrow">Исходный TOML</p>
          <h2>Текст конфига Telemt</h2>
        </div>

        <div className="editor-panel-controls">
          <div className="editor-status-strip">
            <span className={`editor-state-badge ${section.editorHasContent ? 'ready' : 'idle'}`}>{section.editorVersionLabel}</span>
            <span className={`editor-state-badge ${section.unsavedEditorChanges ? 'warning' : 'synced'}`}>{section.editorSyncLabel}</span>
          </div>

          <div className="button-row">
            <button className="secondary-button" disabled={section.actionBusy || section.configLoading} onClick={section.onRollbackEditor} title="Вернуть текст редактора к последней сохраненной ревизии." type="button">
              Откатить редактор
            </button>
            <button
              className="primary-button"
              disabled={section.actionBusy || section.configLoading || section.editorText.trim() === ''}
              onClick={section.onSaveRevision}
              title="Сохранить текст из редактора как новую ревизию в панели. На сервере пока ничего не меняется."
              type="button"
            >
              {section.actionBusy ? 'Выполняется...' : 'Сохранить ревизию'}
            </button>
          </div>
        </div>
      </div>

      {section.configLoading ? <p className="panel-note">Загрузка состояния конфига...</p> : null}
      {section.configError ? <p className="inline-error">{section.configError}</p> : null}
      {section.configWarning ? <p className="inline-warning">{section.configWarning}</p> : null}
      {section.configNotice ? <p className="inline-success">{section.configNotice}</p> : null}

      {!section.editorHasContent ? (
        <div className="editor-empty-note">
          <strong>Редактор пока пустой</strong>
          <p>{section.editorEmptyHint}</p>
          <div className="button-row">
            <button className="primary-button" disabled={section.actionBusy || section.configLoading} onClick={section.onGenerateDraft} title="Собрать новый config.toml из полей формы и сохранить как ревизию в панели." type="button">
              {section.actionBusy ? 'Выполняется...' : 'Сгенерировать TOML'}
            </button>
          </div>
        </div>
      ) : null}

      <div className={`editor-frame ${!section.editorHasContent ? 'empty' : ''}`}>
        <div className="editor-frame-header">
          <span className="editor-frame-label">config.toml</span>
          <span className="editor-frame-path">{section.editorConfigPath}</span>
        </div>

        <textarea
          className={`code-editor ${!section.editorHasContent ? 'code-editor-empty' : ''}`}
          onChange={(event) => section.onChangeEditorText(event.target.value)}
          placeholder="Здесь появится config.toml. Нажмите «Подтянуть MTProto», чтобы открыть файл с сервера, или «Сгенерировать», чтобы собрать новый TOML из полей выше."
          spellCheck="false"
          value={section.editorText}
        />
      </div>

      <div className="editor-footer">
        <span>{section.editorFooterLabel}</span>
        <span>{section.editorConfigPath}</span>
      </div>
    </section>
  );
}
