import { useEffect, useState } from 'react';

import { operationsApi } from '../../api/operationsApi.js';
import {
  buildLogsPage,
  clampLogsPageIndex,
  formatLogLineCount,
  getNewestLogsPageIndex,
} from '../../server-operations.js';
import { describeApiError } from '../../api/api-errors.js';

export function useLogsController({
  selectedServerId,
  deployDraft,
  operationQueryAuthReady,
  operationAuthHelp,
  logsOutputRef,
  syncSavedKeyPathFromDraft,
}) {
  const [logsData, setLogsData] = useState(null);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsError, setLogsError] = useState('');
  const [logsNotice, setLogsNotice] = useState('');
  const [logsLiveEnabled, setLogsLiveEnabled] = useState(false);
  const [logsStreamState, setLogsStreamState] = useState('idle');
  const [logsWindowSize, setLogsWindowSize] = useState(300);
  const [logsPageSize, setLogsPageSize] = useState(100);
  const [logsPageIndex, setLogsPageIndex] = useState(0);

  useEffect(() => {
    setLogsData(null);
    setLogsLoading(false);
    setLogsError('');
    setLogsNotice('');
    setLogsLiveEnabled(false);
    setLogsStreamState('idle');
    setLogsPageIndex(0);
  }, [selectedServerId]);

  useEffect(() => {
    if (!logsLiveEnabled || !selectedServerId) {
      setLogsStreamState('idle');
      return undefined;
    }

    if (!operationQueryAuthReady) {
      setLogsStreamState('error');
      setLogsError(operationAuthHelp);
      return undefined;
    }

    setLogsError('');
    setLogsNotice('');
    setLogsStreamState('connecting');

    const source = new EventSource(operationsApi.getLogsStreamUrl(selectedServerId, deployDraft, { tail: logsWindowSize }));

    source.onopen = () => {
      setLogsStreamState('live');
      setLogsNotice(`Live-стрим подключен. Панель держит только последние ${logsWindowSize} строк и не подгружает полный журнал.`);
    };

    source.addEventListener('logs', (event) => {
      try {
        const payload = JSON.parse(event.data);
        setLogsData(payload);
        setLogsPageIndex(getNewestLogsPageIndex(payload?.result?.stdout || '', logsPageSize));
        setLogsError('');
        setLogsStreamState('live');
      } catch (error) {
        setLogsStreamState('error');
        setLogsError(error instanceof Error ? error.message : 'Не удалось разобрать данные live-логов.');
      }
    });

    source.addEventListener('stream-error', (event) => {
      try {
        const payload = JSON.parse(event.data);
        setLogsStreamState('error');
        setLogsError(describeApiError(payload));
      } catch (error) {
        setLogsStreamState('error');
        setLogsError(error instanceof Error ? error.message : 'Live-стрим логов завершился ошибкой.');
      }
      source.close();
    });

    source.onerror = () => {
      setLogsStreamState('error');
      setLogsError('Live-стрим логов отключился. Повторите попытку, когда снова будет доступна авторизация через путь к SSH-ключу.');
      source.close();
    };

    return () => {
      source.close();
    };
  }, [deployDraft, logsLiveEnabled, logsPageSize, logsWindowSize, operationAuthHelp, operationQueryAuthReady, selectedServerId]);

  useEffect(() => {
    setLogsPageIndex((current) => clampLogsPageIndex(logsData?.result?.stdout || '', logsPageSize, current));
  }, [logsData?.result?.stdout, logsPageSize]);

  useEffect(() => {
    if (!logsLiveEnabled) {
      return;
    }
    if (logsPageIndex !== getNewestLogsPageIndex(logsData?.result?.stdout || '', logsPageSize)) {
      return;
    }

    const element = logsOutputRef.current;
    if (!element) {
      return;
    }
    element.scrollTop = element.scrollHeight;
  }, [logsData?.fetched_at, logsData?.result?.stdout, logsLiveEnabled, logsOutputRef, logsPageIndex, logsPageSize]);

  async function handleLoadLogs(options = {}) {
    if (!selectedServerId) {
      return;
    }

    if (!operationQueryAuthReady) {
      setLogsError(operationAuthHelp);
      return;
    }

    const { silent = false } = options;
    setLogsLoading(true);
    setLogsError('');
    if (!silent) {
      setLogsNotice('');
    }

    try {
      const payload = await operationsApi.getLogs(selectedServerId, deployDraft, { tail: logsWindowSize });

      setLogsData(payload?.logs || null);
      setLogsPageIndex(getNewestLogsPageIndex(payload?.logs?.result?.stdout || '', logsPageSize));
      syncSavedKeyPathFromDraft();
      if (!silent) {
        setLogsNotice(`Загружен только последний фрагмент журнала: до ${logsWindowSize} строк Telemt. Полный журнал здесь не подгружается.`);
      }
    } catch (error) {
      setLogsError(error instanceof Error ? error.message : 'Не удалось загрузить логи сервера.');
    } finally {
      setLogsLoading(false);
    }
  }

  function handleLogsWindowSizeChange(value) {
    setLogsWindowSize(Number.parseInt(value, 10) || 300);
  }

  function handleLogsPageSizeChange(value) {
    const nextPageSize = Number.parseInt(value, 10) || 100;
    setLogsPageSize(nextPageSize);
    setLogsPageIndex(getNewestLogsPageIndex(logsData?.result?.stdout || '', nextPageSize));
  }

  const logsPage = buildLogsPage(logsData?.result?.stdout || '', logsPageSize, logsPageIndex);
  const logsWindowSummary = logsLiveEnabled ? `Live-окно ${logsWindowSize} строк` : `Окно ${logsWindowSize} строк`;
  const logsPageSummary =
    logsPage.totalLines > 0
      ? `Страница ${logsPage.currentPage} из ${logsPage.totalPages} · строки ${logsPage.startLine}-${logsPage.endLine} из ${formatLogLineCount(logsPage.totalLines)}`
      : 'Логи еще не загружались';
  const logsCoverageNote = logsLiveEnabled
    ? `Live-режим держит только скользящее окно: до ${logsWindowSize} строк Telemt. Полный журнал контейнера в браузер не подгружается.`
    : `Панель загружает не весь журнал контейнера, а только последний фрагмент: до ${logsWindowSize} строк Telemt по SSH.`;

  return {
    logsCoverageNote,
    logsData,
    logsError,
    logsLiveEnabled,
    logsLoading,
    logsNotice,
    logsPage,
    logsPageIndex,
    logsPageSize,
    logsPageSummary,
    logsStreamState,
    logsWindowSize,
    logsWindowSummary,
    handleLoadLogs,
    handleLogsPageSizeChange,
    handleLogsWindowSizeChange,
    setLogsLiveEnabled,
    setLogsPageIndex,
  };
}
