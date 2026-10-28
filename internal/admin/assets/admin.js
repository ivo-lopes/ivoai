/* Local read-only console. Authentication belongs to the backend's HttpOnly
   cookie; this file never receives, stores or forwards OAuth tokens. */
(() => {
  'use strict';

  const API = '/api/admin/v1';
  const MAX_RESPONSE_BYTES = 1024 * 1024;
  const $ = (id) => document.getElementById(id);
  const number = new Intl.NumberFormat('pt-BR');
  const date = new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeStyle: 'short' });
  const stateLabels = {
    planned: 'Planejada', primary: 'Principal', queued: 'Na fila', starting: 'Iniciando',
    running: 'Em execução', degraded: 'Degradada', stopping: 'Encerrando', completed: 'Concluída',
    failed: 'Falhou', blocked: 'Bloqueada', waiting_for_quota: 'Aguardando cota',
  };
  const executorLabels = { codex: 'Codex', claude: 'Claude Code', antigravity: 'Antigravity', opencode: 'OpenCode (legado)' };
  const modeLabels = { direct: 'Direto', orchestrated: 'Orquestrado', auto: 'Automático' };
  const modelSources = {
    runtime_verified: 'Verificado na execução', capability_registry: 'Catálogo de capacidades',
    argument: 'Argumento de execução', configured: 'Configuração', default: 'Padrão',
    unsupported: 'Não suportado', unknown: 'Não confirmado',
  };
  const connectionLabels = { codex: 'Codex', claude: 'Claude Code', server: 'Servidor IVOAI', external_mcp: 'Servidores MCP externos' };
  const knownViews = ['sessions', 'components', 'connections'];
  let sessions = [];
  let loading = false;
  let selectedSession = null;
  let eventsRequest = null;
  let requestGeneration = 0;

  function node(tag, className, text) {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text !== undefined) element.textContent = String(text);
    return element;
  }

  function text(value, fallback = 'Não informado') {
    return typeof value === 'string' && value.trim() ? value.slice(0, 240) : fallback;
  }

  function measured(value) {
    return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? number.format(value) : 'Sem medição';
  }

  function timestamp(value) {
    if (typeof value !== 'string' || !value || value.startsWith('0001-')) return 'Sem medição';
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? 'Sem medição' : date.format(parsed);
  }

  function status(value) {
    const tone = ['running', 'completed'].includes(value) ? 'ok'
      : ['failed', 'blocked'].includes(value) ? 'error'
        : ['degraded', 'waiting_for_quota'].includes(value) ? 'warning' : 'neutral';
    return node('span', `status status-${tone}`, stateLabels[value] || 'Não informado');
  }

  async function request(path, options = {}) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);
    const outerSignal = options.signal;
    const abort = () => controller.abort();
    if (outerSignal) {
      outerSignal.addEventListener('abort', abort, { once: true });
      if (outerSignal.aborted) controller.abort();
    }
    try {
      const response = await fetch(path, {
        method: options.method || 'GET', credentials: 'same-origin', cache: 'no-store',
        mode: 'same-origin', redirect: 'error',
        headers: { Accept: 'application/json' }, signal: controller.signal,
      });
      if (!response.ok) {
        await response.body?.cancel();
        const failure = new Error('request_failed');
        failure.status = response.status;
        throw failure;
      }
      if (response.status === 204) return null;
      if (!response.headers.get('content-type')?.includes('application/json')) throw new Error('invalid_response');
      const reader = response.body.getReader();
      const decoder = new TextDecoder('utf-8', { fatal: true });
      let bytes = 0;
      let body = '';
      try {
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          bytes += value.byteLength;
          if (bytes > MAX_RESPONSE_BYTES) throw new Error('response_too_large');
          body += decoder.decode(value, { stream: true });
        }
        body += decoder.decode();
        return JSON.parse(body);
      } finally {
        await reader.cancel().catch(() => {});
      }
    } finally {
      clearTimeout(timeout);
      outerSignal?.removeEventListener('abort', abort);
    }
  }

  function announce(message) { $('announcement').textContent = message; }

  function error(message) {
    $('error').textContent = message;
    $('error').hidden = !message;
  }

  function requireAccess(expired = false) {
    requestGeneration += 1;
    eventsRequest?.abort();
    if ($('session-dialog').open) $('session-dialog').close();
    selectedSession = null;
    sessions = [];
    for (const id of ['sessions-body', 'components-list', 'connections-list', 'session-facts', 'usage-details', 'events-list']) $(id).replaceChildren();
    $('workspace').hidden = true;
    $('detail-title').textContent = 'Sessão';
    $('session-count').textContent = '';
    $('session-summary').replaceChildren();
    $('events-status').textContent = '';
    $('refresh').hidden = true;
    $('sign-out').hidden = true;
    $('access-panel').hidden = false;
    $('sign-in').hidden = false;
    $('access-help').hidden = false;
    $('access-title').textContent = expired ? 'Autorize o acesso novamente' : 'Acesso administrativo local';
    $('access-description').textContent = expired
      ? 'A autorização expirou ou foi revogada. Os dados desta consulta foram removidos da tela.'
      : 'Autorize este navegador para consultar as sessões e os componentes do IVOAI.';
    $('connection-label').textContent = 'Acesso necessário';
    error('');
    announce('Acesso necessário. Autorize este navegador para continuar.');
  }

  function showView() {
    const target = window.location.hash.slice(1);
    const view = knownViews.includes(target) ? target : 'sessions';
    for (const name of knownViews) $(`${name}-view`).hidden = name !== view;
    for (const link of document.querySelectorAll('[data-view]')) {
      if (link.dataset.view === view) link.setAttribute('aria-current', 'page');
      else link.removeAttribute('aria-current');
    }
    const titles = { sessions: 'Sessões', components: 'Componentes', connections: 'Conexões' };
    $('page-title').textContent = titles[view];
    document.title = `${titles[view]} · IVOAI`;
  }

  function populateFilter(id, values, labels) {
    const select = $(id);
    const previous = select.value;
    select.replaceChildren(select.options[0]);
    for (const value of [...new Set(values.filter((v) => typeof v === 'string'))].sort()) {
      const option = node('option', '', labels[value] || 'Não informado');
      option.value = value;
      select.append(option);
    }
    select.value = [...select.options].some((option) => option.value === previous) ? previous : 'all';
  }

  function usageAvailable(usage) {
    return usage && usage.basis === 'client_reported' && usage.scope === 'cli_invocation'
      && usage.source === 'codex_exec_turn_completed' && usage.cached_input_included_in_input === true;
  }

  function renderSessions() {
    const query = $('session-search').value.toLocaleLowerCase('pt-BR');
    const state = $('session-state').value;
    const executor = $('session-executor').value;
    const filtered = sessions.filter((session) => {
      const searchable = `${text(session.id, '')} ${executorLabels[session.executor] || ''}`.toLocaleLowerCase('pt-BR');
      return searchable.includes(query) && (state === 'all' || state === session.state) && (executor === 'all' || executor === session.executor);
    });
    const counts = new Map();
    for (const session of sessions) counts.set(session.state, (counts.get(session.state) || 0) + 1);
    $('session-summary').replaceChildren();
    for (const [key, count] of counts) {
      const item = node('span');
      item.append(node('strong', '', number.format(count)), document.createTextNode(stateLabels[key] || 'Não informado'));
      $('session-summary').append(item);
    }
    $('session-count').textContent = `${number.format(filtered.length)} de ${number.format(sessions.length)}`;
    const body = $('sessions-body');
    body.replaceChildren();
    for (const session of filtered) {
      const row = node('tr');
      const first = node('td');
      const open = node('button', 'session-id', text(session.id));
      open.type = 'button';
      open.addEventListener('click', () => openSession(session));
      first.append(open);
      const executorCell = node('td', '', executorLabels[session.executor] || 'Não informado');
      executorCell.append(node('span', 'cell-secondary', modeLabels[session.mode] || 'Modo não informado'));
      const stateCell = node('td');
      stateCell.append(status(session.state));
      const activity = node('td', '', timestamp(session.updated_at));
      const usage = node('td', '', usageAvailable(session.latest_invocation_usage)
        ? `${measured(session.latest_invocation_usage.input_tokens)} entrada` : 'Sem medição');
      if (usageAvailable(session.latest_invocation_usage)) usage.append(node('span', 'cell-secondary', 'Informado pelo cliente'));
      const detailCell = node('td');
      const detail = node('button', 'button row-detail', 'Ver');
      detail.type = 'button';
      detail.setAttribute('aria-label', `Ver detalhes da sessão ${text(session.id)}`);
      detail.addEventListener('click', () => openSession(session));
      detailCell.append(detail);
      row.append(first, executorCell, stateCell, activity, usage, detailCell);
      body.append(row);
    }
    $('sessions-empty').hidden = filtered.length !== 0;
    body.closest('.table-frame').hidden = filtered.length === 0;
    $('sessions-empty-description').textContent = sessions.length
      ? 'Ajuste a busca ou os filtros para encontrar outra sessão.'
      : 'As sessões aparecem aqui depois de serem registradas pelo IVOAI.';
  }

  function renderComponents(data) {
    const list = $('components-list');
    list.replaceChildren();
    for (const component of data.components) {
      const row = node('article', 'inventory-row');
      const heading = node('div');
      heading.append(node('h3', 'inventory-name', text(component.name)), node('p', 'inventory-subtitle', component.version ? `Versão ${text(component.version)}` : 'Versão não informada'));
      const installed = node('div');
      installed.append(node('span', `status status-${component.installed ? 'ok' : 'neutral'}`, component.installed ? 'Instalado' : 'Não instalado'));
      installed.append(node('p', 'inventory-facts', component.managed ? 'Gerenciado pelo IVOAI' : 'Sem gestão informada'));
      row.append(heading, installed, node('p', 'inventory-description', 'Disponibilidade de execução sem medição. A instalação não confirma integração ou certificação.'));
      list.append(row);
    }
    if (!data.components.length) list.append(empty('Nenhum componente informado', 'O serviço não retornou componentes nesta consulta.'));
    if (data.partial) list.append(node('p', 'section-note', 'Consulta parcial: alguns componentes não puderam ser consultados.'));
  }

  function renderConnections(data) {
    const list = $('connections-list');
    list.replaceChildren();
    for (const connection of data.connections) {
      const row = node('article', 'inventory-row');
      const heading = node('div');
      heading.append(node('h3', 'inventory-name', connectionLabels[connection.name] || 'Conexão'), node('p', 'inventory-subtitle', connection.configured ? 'Configuração registrada' : 'Sem configuração registrada'));
      const state = node('div');
      state.append(node('span', `status status-${connection.enabled ? 'ok' : 'neutral'}`, connection.enabled ? 'Habilitada' : 'Desabilitada'));
      if (connection.name === 'external_mcp') state.append(node('p', 'inventory-facts', `${measured(connection.count)} registros`));
      row.append(heading, state, node('p', 'inventory-description', 'Conectividade e autenticação sem medição. Esta consulta não inicia o serviço.'));
      list.append(row);
    }
    if (!data.connections.length) list.append(empty('Nenhuma conexão informada', 'O serviço não retornou conexões nesta consulta.'));
    if (data.partial) list.append(node('p', 'section-note', 'Consulta parcial: algumas conexões não puderam ser consultadas.'));
  }

  function empty(title, description) {
    const block = node('div', 'empty-state');
    block.append(node('h3', '', title), node('p', '', description));
    return block;
  }

  function fact(label, value) {
    const entry = node('div');
    entry.append(node('dt', '', label), typeof value === 'string' ? node('dd', '', value) : (() => { const dd = node('dd'); dd.append(value); return dd; })());
    return entry;
  }

  function renderUsage(usage) {
    const container = $('usage-details');
    container.replaceChildren();
    if (!usageAvailable(usage)) {
      container.append(node('p', 'section-note', 'Sem medição de consumo para a última execução. Qualidade, cotas e economia não foram avaliadas nesta tela.'));
      return;
    }
    const values = node('dl', 'usage-grid');
    for (const [label, value] of [['Tokens de entrada', usage.input_tokens], ['Tokens de saída', usage.output_tokens], ['Entrada em cache', usage.cached_input_tokens]]) values.append(fact(label, measured(value)));
    container.append(values, node('p', 'section-note', 'Informado pelo cliente Codex para a última execução da CLI; não é o total da sessão. A entrada em cache já está incluída nos tokens de entrada. Qualidade e economia não foram avaliadas.'));
  }

  function openSession(session) {
    selectedSession = session;
    $('detail-title').textContent = text(session.id);
    $('session-facts').replaceChildren(
      fact('Executor', executorLabels[session.executor] || 'Não informado'), fact('Estado', status(session.state)),
      fact('Modo', modeLabels[session.mode] || 'Não informado'), fact('Modelo', text(session.model?.name)),
      fact('Origem do modelo', modelSources[session.model?.source] || 'Não confirmado'), fact('Iniciada em', timestamp(session.started_at)),
      fact('Última atividade', timestamp(session.updated_at)), fact('Workers / tarefas', `${measured(session.worker_count)} / ${measured(session.task_count)}`),
    );
    renderUsage(session.latest_invocation_usage);
    $('events-list').replaceChildren();
    if (!$('session-dialog').open) $('session-dialog').showModal();
    loadEvents();
  }

  async function loadEvents() {
    eventsRequest?.abort();
    const controller = new AbortController();
    eventsRequest = controller;
    const session = selectedSession;
    if (!session) return;
    $('refresh-events').disabled = true;
    $('events-status').textContent = 'Consultando eventos…';
    try {
      const data = await request(`${API}/sessions/${encodeURIComponent(session.id)}/events?after=0`, { signal: controller.signal });
      if (controller.signal.aborted || selectedSession !== session || !$('session-dialog').open) return;
      if (!Array.isArray(data.events)) throw new Error('invalid_response');
      $('events-list').replaceChildren();
      for (const event of data.events) {
        const item = node('li');
        const content = node('div');
        content.append(node('p', 'event-title', `${text(event.category)} / ${text(event.operation)}`));
        const details = [timestamp(event.observed_at), stateLabels[event.state] || text(event.state, 'Estado não informado')];
        if (typeof event.duration_ms === 'number' && event.duration_ms >= 0) details.push(`${measured(event.duration_ms)} ms`);
        if (typeof event.cache_hit === 'boolean') details.push(event.cache_hit ? 'Cache reutilizado' : 'Sem reutilização de cache');
        content.append(node('p', 'event-meta', details.join(' · ')));
        item.append(node('span', 'event-dot'), content);
        $('events-list').append(item);
      }
      $('events-status').textContent = data.gap ? 'Histórico parcial: eventos anteriores não estão disponíveis.' : data.partial ? 'Consulta parcial de eventos.' : data.events.length ? `${number.format(data.events.length)} ${data.events.length === 1 ? 'evento' : 'eventos'} nesta consulta.` : 'Nenhum evento disponível para esta sessão.';
    } catch (failure) {
      if (controller.signal.aborted) return;
      if (failure.status === 401 || failure.status === 403) { requireAccess(true); return; }
      if (selectedSession === session && $('session-dialog').open) $('events-status').textContent = 'Não foi possível consultar os eventos. Tente atualizar.';
    } finally { if (eventsRequest === controller) $('refresh-events').disabled = false; }
  }

  async function refresh() {
    if (loading) return;
    loading = true;
    const generation = requestGeneration;
    $('refresh').disabled = true;
    $('refresh').setAttribute('aria-busy', 'true');
    error('');
    try {
      const [overview, sessionData, components, connections] = await Promise.all([
        request(`${API}/overview`), request(`${API}/sessions`), request(`${API}/components`), request(`${API}/connections`),
      ]);
      if (generation !== requestGeneration) return;
      if (overview.schema_version !== 1 || overview.read_only !== true || !Array.isArray(sessionData.sessions) || !Array.isArray(components.components) || !Array.isArray(connections.connections)) throw new Error('invalid_response');
      sessions = sessionData.sessions;
      populateFilter('session-state', sessions.map((session) => session.state), stateLabels);
      populateFilter('session-executor', sessions.map((session) => session.executor), executorLabels);
      renderSessions(); renderComponents(components); renderConnections(connections);
      $('snapshot-time').textContent = `Consultado em ${timestamp(overview.generated_at)}`;
      $('version').textContent = overview.version && overview.version !== 'unknown' ? `v${text(overview.version).replace(/^v/, '')}` : 'Versão não informada';
      const partial = overview.partial || overview.truncated || sessionData.partial || sessionData.truncated || components.partial || connections.partial;
      $('connection-label').textContent = partial ? 'Consulta parcial' : 'Consulta concluída';
      if (partial) error('Consulta parcial: alguns registros não estão disponíveis. Os números exibidos cobrem apenas os dados retornados.');
      $('workspace').hidden = false;
      $('access-panel').hidden = true;
      $('refresh').hidden = false;
      $('sign-out').hidden = false;
      if (selectedSession) {
        const updated = sessions.find((session) => session.id === selectedSession.id);
        if (updated) openSession(updated);
        else { $('session-dialog').close(); selectedSession = null; }
      }
      announce(partial ? 'Dados atualizados parcialmente.' : 'Dados administrativos atualizados.');
    } catch (failure) {
      if (failure.status === 401 || failure.status === 403) { requireAccess(!$('workspace').hidden); return; }
      $('connection-label').textContent = 'Consulta indisponível';
      error('Não foi possível consultar o serviço local. Verifique se o IVOAI está em execução e tente atualizar. Os dados anteriores, quando presentes, não foram atualizados.');
      $('access-title').textContent = 'Serviço indisponível';
      $('access-description').textContent = 'Não foi possível confirmar o acesso ao serviço administrativo.';
      $('refresh').hidden = false;
    } finally {
      loading = false;
      $('refresh').disabled = false;
      $('refresh').removeAttribute('aria-busy');
    }
  }

  async function signOut() {
    $('sign-out').disabled = true;
    requestGeneration += 1;
    try {
      await request('/auth/logout', { method: 'POST' });
      requireAccess();
      announce('Acesso encerrado neste navegador.');
    } catch (failure) {
      if (failure.status === 401 || failure.status === 403) requireAccess(true);
      else error('Não foi possível encerrar o acesso. Tente novamente.');
    }
    finally { $('sign-out').disabled = false; }
  }

  for (const id of ['session-search', 'session-state', 'session-executor']) $(id).addEventListener(id === 'session-search' ? 'input' : 'change', renderSessions);
  $('refresh').addEventListener('click', refresh);
  $('sign-out').addEventListener('click', signOut);
  $('refresh-events').addEventListener('click', loadEvents);
  $('close-detail').addEventListener('click', () => $('session-dialog').close());
  $('session-dialog').addEventListener('close', () => { selectedSession = null; eventsRequest?.abort(); });
  window.addEventListener('hashchange', showView);
  showView();
  request(`${API}/meta`).then((meta) => {
    if (meta.schema_version !== 1 || meta.authentication !== 'oauth_pkce_cookie' || meta.access !== 'local_owner_read_only') throw new Error('incompatible_contract');
    return refresh();
  }).catch(() => {
    $('connection-label').textContent = 'Serviço indisponível';
    $('access-title').textContent = 'Serviço indisponível';
    $('access-description').textContent = 'Não foi possível confirmar o contrato do serviço administrativo. Atualize a página depois de verificar o serviço local.';
  });
})();
