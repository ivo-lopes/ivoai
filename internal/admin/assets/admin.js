/* Administrative views use the existing Core APIs. OAuth tokens remain in
   HttpOnly cookies; CSRF and operation confirmations exist only in tab memory. */
(() => {
  'use strict';

  const baseURL = new URL('../', document.currentScript.src);
  const API = new URL('api/admin/v1', baseURL).pathname;
  const localPath = (path) => new URL(path, baseURL).pathname;
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
  const knownViews = ['sessions', 'components', 'connections', 'administration'];
  const kindLabels = { read: 'Consulta', mutation: 'Alteração', native_handoff: 'Cliente nativo', information: 'Orientação' };
  const groupLabels = { configuration: 'Configuração', config: 'Configuração', policy: 'Políticas', sessions: 'Sessões', session: 'Sessões', capabilities: 'Skills e capacidades', capability: 'Skills e capacidades', components: 'Componentes', maintenance: 'Manutenção', connections: 'Conexões', connect: 'Conexões', mcp: 'Servidores MCP', memory: 'Memória', context: 'Contexto', server: 'Servidor', remote: 'Servidor remoto', project: 'Projeto', profiles: 'Perfis', diagnostics: 'Diagnósticos', tools: 'Ferramentas', other: 'Outras operações' };
  const groupAliases = { auto: 'sessions', launch: 'sessions', session: 'sessions', capability: 'capabilities', config: 'configuration', profile: 'profiles', connect: 'connections', disconnect: 'connections', servers: 'connections', setup: 'maintenance', status: 'maintenance', doctor: 'maintenance', update: 'maintenance', rollback: 'maintenance', uninstall: 'maintenance', version: 'maintenance' };
  const actionGroup = (action) => groupAliases[action.group] || action.group || 'other';
  const optionLabels = { true: 'Habilitado', false: 'Desabilitado', enable: 'Habilitar', disable: 'Desabilitar', off: 'Desativado', on: 'Ativado', auto: 'Automático', read_only: 'Somente leitura', disabled: 'Desabilitado', economic: 'Econômico', balanced: 'Equilibrado', intensive: 'Intensivo', review: 'Revisão', development: 'Desenvolvimento', research: 'Pesquisa', diagnostic: 'Diagnóstico' };
  let actions = [];
  let csrfToken = '';
  let accessMode = 'read';
  let selectedAction = null;
  let actionBusy = false;
  let pendingAction = null;
  let actionExpiryTimer = null;
  let operationHistory = [];
  let catalogFailure = false;
  // UI copy is separate from operation identifiers and never affects dispatch.
  // Explicit human-readable labels supplied by a newer backend take precedence.
  const actionCopy = {
    'auto': ['Continuar no cliente nativo', 'Abra uma sessão gerenciada no terminal. A conversa permanece na interface oficial do cliente.'],
    'capabilities.ponytail': ['Configurar Ponytail', 'Escolha quando o catálogo opcional de capacidades pode ser utilizado.'],
    'capabilities.update': ['Atualizar capacidades', 'Atualize as capacidades gerenciadas preservando suas políticas e proveniência.'],
    'capability.disable': ['Desabilitar capacidade', 'Desabilite uma fonte específica de capacidade.'],
    'capability.enable': ['Habilitar capacidade', 'Habilite uma fonte de capacidade previamente registrada.'],
    'capability.inspect': ['Consultar capacidade', 'Veja a versão, a origem e a política de uma capacidade.'],
    'capability.pin': ['Fixar versão de capacidade', 'Mantenha a revisão atual da capacidade até remover a fixação.'],
    'capability.rollback': ['Restaurar capacidade', 'Retorne à revisão anterior disponível da capacidade.'],
    'capability.unpin': ['Remover fixação de capacidade', 'Permita novas atualizações da capacidade conforme a política existente.'],
    'capability.update': ['Atualizar capacidade', 'Atualize somente a fonte de capacidade escolhida.'],
    'config.auto': ['Configurar execução automática', 'Habilite ou desabilite a orquestração automática.'],
    'config.auto-bootstrap': ['Configurar contexto compartilhado', 'Escolha se a execução pode preparar contexto compartilhado para as tarefas.'],
    'config.auto-checkpoint': ['Configurar checkpoints', 'Escolha se a execução pode registrar checkpoints de continuidade.'],
    'config.auto-escalation': ['Configurar escalonamento', 'Escolha se o fluxo automático pode solicitar uma estratégia mais ampla.'],
    'config.auto-failover': ['Configurar troca de executor', 'Escolha a política de troca de executor; as confirmações existentes continuam obrigatórias.'],
    'config.auto-gate': ['Consultar política de entrada', 'Confira como pedidos naturais são admitidos e quais validações continuam obrigatórias.'],
    'config.auto-parallel': ['Configurar paralelismo', 'Escolha se tarefas independentes podem executar em paralelo.'],
    'config.auto-plan': ['Configurar planejamento', 'Escolha o modo de planejamento das execuções gerenciadas.'],
    'config.auto-planner': ['Configurar planejador', 'Escolha o executor responsável pelo planejamento.'],
    'config.auto-policy': ['Consultar política automática', 'Consulte as opções de política disponíveis para a execução automática.'],
    'config.auto-profiles': ['Consultar perfis de execução', 'Veja os perfis disponíveis e as operações para revisar ou aplicar um perfil.'],
    'config.auto-strategy': ['Configurar estratégia automática', 'Escolha a estratégia permitida para a execução automática.'],
    'config.headroom': ['Configurar Headroom', 'Configure o provedor opcional sem alterar as demais políticas de compressão.'],
    'config.memory': ['Configurar uso de memória', 'Habilite ou desabilite a memória operacional na configuração.'],
    'config.primary': ['Escolher executor principal', 'Defina o executor principal das sessões conforme as capacidades disponíveis.'],
    'config.reviewer': ['Escolher revisor', 'Defina o executor utilizado nas etapas de revisão.'],
    'config.ruflo': ['Configurar Ruflo', 'Configure a integração existente com Ruflo.'],
    'config.session-mode': ['Escolher modo de sessão', 'Defina o modo padrão utilizado ao iniciar novas sessões.'],
    'config.show': ['Consultar configuração', 'Veja uma projeção administrativa sem credenciais da configuração atual.'],
    'config.workers': ['Configurar workers', 'Defina o limite padrão de workers da execução.'],
    'connect.chatgpt': ['Conectar ChatGPT', 'Continue o login pelo fluxo oficial no terminal; o painel não recebe credenciais da assinatura.'],
    'connect.claude': ['Conectar Claude Code', 'Continue o login pelo cliente oficial no terminal.'],
    'connect.list': ['Listar conexões', 'Veja as conexões registradas e seus estados disponíveis.'],
    'connect.server': ['Conectar servidor', 'Registre uma conexão com um servidor IVOAI autorizado.'],
    'disconnect.chatgpt': ['Desconectar ChatGPT', 'Remova a conexão gerenciada conforme o contrato existente.'],
    'disconnect.claude': ['Desconectar Claude Code', 'Remova a conexão gerenciada conforme o contrato existente.'],
    'doctor': ['Executar diagnósticos', 'Verifique o estado atual das dependências e integrações disponíveis.'],
    'doctor.inventory': ['Consultar compatibilidade', 'Colete o inventário sanitizado utilizado na avaliação de compatibilidade.'],
    'doctor.repair-hooks': ['Reparar hooks de memória', 'Repare somente hooks reconhecidamente gerenciados pelo IVOAI.'],
    'launch.claude': ['Continuar no Claude Code', 'Use a interface nativa do Claude Code no terminal.'],
    'launch.codex': ['Continuar no Codex', 'Use a interface nativa do Codex no terminal.'],
    'launch.opencode': ['Continuar no OpenCode legado', 'Consulte o encaminhamento compatível do cliente legado no terminal.'],
    'mcp.add': ['Adicionar servidor MCP', 'Registre um servidor MCP e seu endpoint autorizado.'],
    'mcp.auth': ['Definir autenticação MCP', 'Atualize a credencial privada do servidor escolhido sem exibi-la nos resultados.'],
    'mcp.auth.remove': ['Remover autenticação MCP', 'Remova a credencial privada registrada para o servidor escolhido.'],
    'mcp.direct-policy': ['Configurar política MCP direta', 'Defina as permissões da integração MCP nos modos diretos.'],
    'mcp.enable': ['Habilitar ou desabilitar MCP', 'Altere a disponibilidade do servidor MCP registrado.'],
    'mcp.header': ['Definir cabeçalho MCP', 'Configure um cabeçalho privado da conexão sem exibir seu valor.'],
    'mcp.list': ['Listar servidores MCP', 'Veja os servidores MCP configurados.'],
    'mcp.policy': ['Configurar política MCP', 'Defina as permissões concedidas ao servidor MCP.'],
    'mcp.remove': ['Remover servidor MCP', 'Remova o registro do servidor MCP escolhido.'],
    'mcp.test': ['Testar servidor MCP', 'Verifique a conexão com o servidor escolhido.'],
    'mcp.tools': ['Consultar ferramentas MCP', 'Veja as ferramentas e capacidades expostas pela conexão.'],
    'memory.configure': ['Configurar memória operacional', 'Configure a memória operacional existente sem criar outro sistema de memória.'],
    'memory.status': ['Consultar memória operacional', 'Veja o estado informado pela memória operacional.'],
    'policy.concurrency': ['Configurar concorrência', 'Escolha a política para execuções concorrentes.'],
    'policy.confirmation': ['Configurar confirmações', 'Defina as confirmações exigidas nas operações gerenciadas.'],
    'policy.low-quota': ['Configurar limite de cota', 'Defina o limiar utilizado pela política de cota baixa.'],
    'policy.mcp': ['Configurar acesso MCP', 'Defina a política geral de ferramentas MCP.'],
    'policy.primary': ['Configurar política do Primary', 'Defina a política aplicada ao executor principal.'],
    'policy.provider': ['Configurar preferência de provedor', 'Escolha a preferência de provedor usada pelo roteamento existente.'],
    'policy.sources': ['Configurar fontes de conhecimento', 'Defina a política de seleção das fontes de conhecimento.'],
    'policy.worker-cap': ['Configurar limite de workers', 'Defina o teto permitido para workers de uma execução automática.'],
    'policy.writes': ['Configurar escritas paralelas', 'Defina se a política permite alterações paralelas nos escopos autorizados.'],
    'profile.apply': ['Aplicar perfil de execução', 'Revise e aplique um dos perfis disponíveis.'],
    'profile.preview': ['Revisar perfil de execução', 'Consulte as alterações propostas por um perfil sem aplicá-las.'],
    'profile.restore': ['Restaurar perfil de execução', 'Restaure o perfil anterior disponível.'],
    'profile.show': ['Consultar perfil de execução', 'Veja os valores registrados para o perfil.'],
    'project.init': ['Inicializar projeto', 'Inicialize a configuração do projeto no escopo autorizado.'],
    'project.status': ['Consultar projeto', 'Veja a identidade e a configuração do projeto atual.'],
    'remote.connector.list': ['Listar conectores remotos', 'Consulte os conectores expostos pelo servidor conectado.'],
    'remote.doctor': ['Diagnosticar servidor remoto', 'Consulte os diagnósticos disponíveis no servidor conectado.'],
    'remote.status': ['Consultar servidor remoto', 'Veja o estado disponível do servidor conectado.'],
    'rollback': ['Restaurar versão anterior', 'Restaure o conjunto gerenciado anterior conforme a política de rollback.'],
    'server.backup': ['Criar backup do servidor', 'Grave um backup em um caminho privado autorizado.'],
    'server.connector.add': ['Adicionar conector documental', 'Registre um corpus aprovado para o serviço de contexto existente.'],
    'server.connector.list': ['Listar conectores documentais', 'Veja os conectores registrados no serviço de contexto.'],
    'server.connector.remove': ['Remover conector documental', 'Remova o conector escolhido conforme as regras existentes.'],
    'server.context.document': ['Recuperar documento exato', 'Consulte um documento pelo identificador fornecido pelo contexto.'],
    'server.context.ingest': ['Ingerir documentos', 'Execute a ingestão autorizada pelo conector existente.'],
    'server.context.search': ['Buscar documentos', 'Consulte o índice documental no escopo autorizado.'],
    'server.context.status': ['Consultar contexto documental', 'Veja o estado do serviço de contexto e de seu índice.'],
    'server.doctor': ['Diagnosticar servidor local', 'Verifique os serviços administrados nesta instalação.'],
    'server.enrollment.create': ['Criar código de conexão', 'Crie um código de uso único para conectar um cliente autorizado.'],
    'server.enrollment.list': ['Listar códigos de conexão', 'Consulte metadados dos códigos emitidos sem recuperar seus segredos.'],
    'server.enrollment.revoke': ['Revogar código de conexão', 'Revogue o identificador de ativação escolhido.'],
    'server.gateway.configure': ['Configurar endereço público', 'Defina o endereço HTTPS público utilizado pelo gateway existente.'],
    'server.logs': ['Consultar logs do servidor', 'Veja os diagnósticos sanitizados disponíveis.'],
    'server.memory.status': ['Consultar memória do servidor', 'Veja o estado da memória no servidor administrado.'],
    'server.restart': ['Reiniciar servidor', 'Reinicie os serviços existentes com as permissões do processo administrativo.'],
    'server.restore': ['Restaurar backup do servidor', 'Restaure um arquivo de backup autorizado após revisar a operação.'],
    'server.setup': ['Configurar servidor', 'Prepare os serviços usando as permissões existentes do administrador.'],
    'server.start': ['Iniciar servidor', 'Inicie os serviços administrados nesta instalação.'],
    'server.status': ['Consultar servidor local', 'Veja o estado dos serviços administrados nesta instalação.'],
    'server.stop': ['Parar servidor', 'Pare os serviços administrados após revisar a alteração.'],
    'server.web-access.create': ['Criar acesso web', 'Crie um código de acesso web e guarde o segredo exibido uma única vez.'],
    'server.web-access.list': ['Listar acessos web', 'Consulte os metadados dos acessos web emitidos.'],
    'server.web-access.revoke': ['Revogar acesso web', 'Revogue o acesso identificado conforme a política existente.'],
    'servers.add': ['Adicionar conexão com servidor', 'Registre um servidor HTTPS usando um código de conexão autorizado.'],
    'servers.edit': ['Editar conexão com servidor', 'Atualize a finalidade, o grupo e a prioridade do servidor registrado.'],
    'servers.list': ['Listar servidores conectados', 'Veja as conexões registradas com servidores IVOAI.'],
    'servers.manage': ['Consultar conexão com servidor', 'Veja os detalhes administrativos do servidor escolhido.'],
    'servers.re-enroll': ['Renovar conexão com servidor', 'Renove uma conexão usando um novo código autorizado.'],
    'servers.remove': ['Remover conexão com servidor', 'Remova o registro da conexão escolhida.'],
    'servers.test': ['Testar conexão com servidor', 'Verifique a disponibilidade da conexão escolhida.'],
    'servers.toggle': ['Habilitar ou desabilitar servidor', 'Altere a disponibilidade de uma conexão registrada.'],
    'session.continuity': ['Consultar continuidade da sessão', 'Veja o estado de continuidade registrado sem retomar a conversa.'],
    'session.direct.claude': ['Continuar diretamente no Claude', 'Abra o cliente nativo em modo direto e explícito no terminal.'],
    'session.direct.codex': ['Continuar diretamente no Codex', 'Abra o cliente nativo em modo direto e explícito no terminal.'],
    'session.direct.opencode': ['Continuar diretamente no OpenCode', 'Consulte o encaminhamento legado em modo direto no terminal.'],
    'session.list': ['Listar sessões', 'Veja as sessões registradas no escopo administrativo.'],
    'session.monitor': ['Monitorar sessão', 'Consulte os eventos administrativos registrados da sessão.'],
    'session.orchestrated.claude': ['Continuar no Claude gerenciado', 'Abra uma sessão gerenciada no cliente nativo, conforme suas capacidades certificadas.'],
    'session.orchestrated.codex': ['Continuar no Codex gerenciado', 'Abra uma sessão gerenciada no cliente nativo, conforme suas capacidades certificadas.'],
    'session.scope.adopt': ['Adotar escopo de sessão', 'Revise e adote um escopo de conhecimento para a sessão.'],
    'session.scope.restore': ['Restaurar escopo de sessão', 'Restaure o escopo previamente registrado no backup autorizado.'],
    'session.stop': ['Parar sessão', 'Solicite o encerramento da sessão registrada.'],
    'setup': ['Preparar instalação', 'Instale ou repare os componentes gerenciados conforme o contrato existente.'],
    'status': ['Consultar estado da instalação', 'Veja a disponibilidade registrada dos componentes e conexões.'],
    'uninstall': ['Desinstalar componentes gerenciados', 'Remova somente os componentes gerenciados conforme as regras da instalação.'],
    'update': ['Atualizar instalação', 'Atualize o conjunto gerenciado após revisar a operação.'],
    'update.dry-run': ['Revisar atualização', 'Consulte a atualização proposta sem instalar novos componentes.'],
    'version': ['Consultar versão', 'Veja a versão instalada do IVOAI.'],
  };
  const fieldLabels = {
    'Absolute backup archive path': 'Caminho absoluto do backup', 'Absolute private backup archive path': 'Caminho absoluto e privado do backup',
    'Activation ID': 'Identificador do acesso', 'Activation lifetime, maximum 24h': 'Validade do código (máximo de 24 horas)',
    'Alias': 'Apelido', 'Approved absolute corpus path': 'Caminho absoluto do corpus aprovado', 'Bearer credential': 'Credencial privada',
    'Capability source ID': 'Identificador da capacidade', 'Code': 'Código', 'Configured connector name': 'Nome do conector registrado',
    'Document query': 'Consulta documental', 'Enrollment code': 'Código de conexão', 'Exact document ID': 'Identificador exato do documento',
    'Group': 'Grupo', 'HTTP header name': 'Nome do cabeçalho HTTP', 'HTTPS endpoint': 'Endpoint HTTPS', 'HTTPS server URL': 'Endereço HTTPS do servidor',
    'Knowledge source': 'Fonte de conhecimento', 'Name': 'Nome', 'Numeric listener IP:port': 'IP e porta do listener',
    'Owned scope backup path': 'Caminho do backup de escopo', 'Priority': 'Prioridade', 'Private header value': 'Valor privado do cabeçalho',
    'Profile': 'Perfil', 'Public HTTPS origin': 'Endereço HTTPS público', 'Purpose': 'Finalidade', 'Session ID': 'Identificador da sessão',
    'State': 'Estado', 'Type': 'Tipo', 'Url': 'Endereço', 'Value': 'Valor',
  };
  function titleForAction(action) {
    const fallback = action.title === action.id || action.title === action.id.replaceAll('.', ' · ');
    return fallback && actionCopy[action.id] ? actionCopy[action.id][0] : text(action.title, action.id);
  }
  function descriptionForAction(action) {
    const fallback = action.title === action.id || action.title === action.id.replaceAll('.', ' · ');
    return fallback && actionCopy[action.id] ? actionCopy[action.id][1] : text(action.description, 'Consulte as opções desta operação.');
  }
  const fieldLabel = (field) => fieldLabels[field.label] || text(field.label, field.name);

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
    const timeout = setTimeout(() => controller.abort(), options.timeout || 10000);
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
        headers: { Accept: 'application/json', ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...(options.csrf ? { 'X-IVOAI-CSRF': options.csrf } : {}) },
        ...(options.body ? { body: JSON.stringify(options.body) } : {}), signal: controller.signal,
      });
      if (!response.ok) {
        const failure = new Error('request_failed');
        failure.status = response.status;
        await response.body?.cancel();
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
    if ($('action-dialog').open) $('action-dialog').close();
    clearTimeout(actionExpiryTimer);
    selectedAction = null; pendingAction = null; actionBusy = false;
    csrfToken = ''; accessMode = 'read'; actions = []; operationHistory = [];
    for (const id of ['actions-list', 'action-fields', 'action-review-values', 'action-result-output', 'operation-history']) $(id).replaceChildren();
    $('operation-history-section').hidden = true;
    $('action-count').textContent = '';
    $('action-error').hidden = true;
    $('action-progress').textContent = '';
    $('action-result-request').textContent = '';
    $('action-result-state').textContent = '';
    $('action-confirmed').checked = false;
    clearOneTimeCode();
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
    $('access-title').textContent = expired ? 'Autorize o acesso novamente' : 'Acesso administrativo';
    $('access-description').textContent = expired
      ? 'A autorização expirou ou foi revogada. Os dados desta consulta foram removidos da tela.'
      : 'Autorize este navegador para consultar ou administrar o IVOAI conforme as permissões concedidas.';
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
    const titles = { sessions: 'Sessões', components: 'Componentes', connections: 'Conexões', administration: 'Administração' };
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

  async function loadAdministration(generation) {
    try {
      const [catalog, csrf] = await Promise.all([request(`${API}/actions`), request(`${API}/csrf`)]);
      if (generation !== requestGeneration) return;
      if (catalog.schema_version !== 1 || !Array.isArray(catalog.actions) || catalog.actions.length > 512
        || !['read', 'manage'].includes(csrf.access) || typeof csrf.csrf_token !== 'string' || csrf.csrf_token.length > 2048 || (csrf.access === 'manage' && !csrf.csrf_token)) throw new Error('invalid_administrative_contract');
      const ids = new Set();
      for (const action of catalog.actions) {
        if (!action || typeof action.id !== 'string' || !/^[a-z][a-z0-9._-]{0,100}$/.test(action.id)
          || ids.has(action.id) || !Object.hasOwn(kindLabels, action.kind) || !Array.isArray(action.fields)
          || action.fields.length > 32 || typeof action.available !== 'boolean') throw new Error('invalid_action');
        ids.add(action.id);
      }
      actions = catalog.actions;
      csrfToken = csrf.csrf_token;
      accessMode = csrf.access;
      catalogFailure = false;
      const select = $('action-group');
      const previous = select.value;
      select.replaceChildren(new Option('Todas as áreas', 'all'));
      for (const group of [...new Set(actions.map(actionGroup))].sort()) select.append(new Option(groupLabels[group] || text(group), group));
      select.value = [...select.options].some((option) => option.value === previous) ? previous : 'all';
      $('access-mode').textContent = accessMode === 'manage' ? 'Acesso de administração' : 'Acesso de leitura';
      $('snapshot-access').textContent = accessMode === 'manage' ? 'Administração autorizada' : 'Somente leitura';
      $('action-access-notice').hidden = accessMode === 'manage';
      $('action-access-notice').textContent = accessMode === 'manage' ? '' : 'Este acesso permite consultas. Para alterar configurações, autorize um acesso de administração com permissão de escrita.';
      renderActions();
    } catch (failure) {
      if (generation !== requestGeneration) return;
      if (failure.status === 401 || failure.status === 403) throw failure;
      actions = []; csrfToken = ''; accessMode = 'read'; catalogFailure = true;
      $('access-mode').textContent = 'Acesso de leitura';
      $('snapshot-access').textContent = 'Somente leitura';
      $('action-access-notice').hidden = false;
      $('action-access-notice').textContent = 'As operações administrativas não puderam ser consultadas. As outras telas continuam disponíveis; atualize para tentar consultar o catálogo novamente.';
      renderActions();
    }
  }

  function renderActions() {
    const query = $('action-search').value.toLocaleLowerCase('pt-BR');
    const group = $('action-group').value;
    const kind = $('action-kind').value;
    const filtered = actions.filter((action) => `${titleForAction(action)} ${descriptionForAction(action)} ${action.id} ${groupLabels[actionGroup(action)] || actionGroup(action)}`.toLocaleLowerCase('pt-BR').includes(query)
      && (group === 'all' || actionGroup(action) === group) && (kind === 'all' || action.kind === kind));
    $('action-count').textContent = `${number.format(filtered.length)} de ${number.format(actions.length)}`;
    const list = $('actions-list');
    list.replaceChildren();
    for (const action of filtered) {
      const row = node('article', 'action-row');
      row.dataset.actionId = action.id;
      const content = node('div', 'action-copy');
      content.append(node('p', 'action-group', groupLabels[actionGroup(action)] || text(actionGroup(action), 'Administração')), node('h3', '', titleForAction(action)), node('p', 'action-description', descriptionForAction(action)));
      const meta = node('div', 'action-row-controls');
      meta.append(node('span', `status status-${action.kind === 'mutation' ? 'warning' : 'neutral'}`, kindLabels[action.kind]));
      const button = node('button', 'button', action.kind === 'mutation' ? 'Configurar' : action.kind === 'read' ? 'Consultar' : 'Ver orientação');
      button.type = 'button';
      button.setAttribute('aria-label', `${button.textContent}: ${titleForAction(action)}`);
      button.disabled = !action.available || (action.kind === 'mutation' && accessMode !== 'manage');
      button.addEventListener('click', () => openAction(action));
      meta.append(button);
      row.append(content, meta);
      if (!action.available || (action.kind === 'mutation' && accessMode !== 'manage')) row.append(node('p', 'action-unavailable', !action.available ? text(action.reason, 'Operação indisponível neste ambiente.') : 'Requer acesso de administração.'));
      list.append(row);
    }
    $('actions-empty').hidden = filtered.length > 0 || catalogFailure;
  }

  function setActionError(message) {
    $('action-error').textContent = message;
    $('action-error').hidden = !message;
  }

  function fieldControl(field, index) {
    if (!field || typeof field.name !== 'string' || !/^[a-z][a-z0-9_]{0,63}$/.test(field.name)
      || ['__proto__', 'constructor', 'prototype'].includes(field.name)) throw new Error('invalid_field');
    const container = node('div', 'field');
    const id = `operation-field-${index}`;
    const label = node('label', '', `${fieldLabel(field)}${field.required ? ' (obrigatório)' : ''}`);
    label.htmlFor = id;
    const type = field.type || 'text';
    let input;
    if (type === 'select' || Array.isArray(field.options)) {
      input = node('select');
      if (!field.required) input.append(new Option('Não alterar / não informado', ''));
      for (const option of (field.options || []).slice(0, 256)) {
        const value = typeof option === 'string' ? option : option.value;
        if (typeof value !== 'string') throw new Error('invalid_option');
        input.append(new Option(typeof option === 'string' ? optionLabels[option] || option : text(option.label, optionLabels[value] || value), value));
      }
    } else if (['textarea', 'json', 'multiline'].includes(type)) {
      input = node('textarea'); input.rows = 5; input.maxLength = 65536;
    } else {
      input = node('input');
      input.type = ['boolean', 'checkbox'].includes(type) ? 'checkbox' : ['integer', 'number'].includes(type) ? 'number' : type === 'password' ? 'password' : type === 'url' ? 'url' : 'text';
      if (input.type === 'number') {
        if (type === 'integer') input.step = '1';
        if (Number.isFinite(field.min)) input.min = field.min;
        if (Number.isFinite(field.max)) input.max = field.max;
      } else if (input.type !== 'checkbox') input.maxLength = type === 'password' ? 8192 : 4096;
      if (input.type === 'checkbox') container.classList.add('checkbox-field');
    }
    input.id = id; input.name = field.name;
    input.required = field.required === true && input.type !== 'checkbox';
    input.autocomplete = 'off'; input.spellcheck = false;
    if (typeof field.default === 'string') input.value = field.default;
    if (input.type === 'checkbox') input.checked = field.default === true || field.default === 'true';
    if (input.type === 'password') input.dataset.sensitive = 'true';
    if (typeof field.placeholder === 'string') input.placeholder = field.placeholder.slice(0, 240);
    container.append(label, input);
    if (field.help) {
      const help = node('p', 'field-help', text(field.help)); help.id = `${id}-help`; input.setAttribute('aria-describedby', help.id); container.append(help);
    }
    return container;
  }

  function openAction(action) {
    if (actionBusy || !action.available || (action.kind === 'mutation' && accessMode !== 'manage')) return;
    clearTimeout(actionExpiryTimer);
    selectedAction = action; pendingAction = null;
    $('action-title').textContent = titleForAction(action);
    $('action-area').textContent = groupLabels[actionGroup(action)] || text(actionGroup(action), 'Administração');
    $('action-description').textContent = descriptionForAction(action);
    $('action-progress').textContent = '';
    $('action-review').hidden = true; $('action-result').hidden = true; $('action-form').hidden = false;
    $('action-confirmed').checked = false; $('action-execute').disabled = true;
    $('action-result-output').replaceChildren(); $('action-review-values').replaceChildren(); $('action-fields').replaceChildren();
    $('action-result-request').textContent = '';
    clearOneTimeCode();
    setActionError('');
    try {
      const names = new Set();
      for (let index = 0; index < action.fields.length; index += 1) {
        if (names.has(action.fields[index].name)) throw new Error('duplicate_field');
        names.add(action.fields[index].name); $('action-fields').append(fieldControl(action.fields[index], index));
      }
      $('action-submit').disabled = false;
    } catch {
      $('action-fields').replaceChildren(); $('action-submit').disabled = true;
      setActionError('O serviço retornou um formulário inválido. Atualize o catálogo antes de continuar.');
    }
    $('action-submit').textContent = action.kind === 'mutation' ? 'Revisar alteração' : action.kind === 'read' ? 'Consultar' : 'Ver orientação';
    if (!$('action-dialog').open) $('action-dialog').showModal();
  }

  function actionInput() {
    const input = Object.create(null);
    for (const field of $('action-fields').querySelectorAll('input,select,textarea')) input[field.name] = field.type === 'checkbox' ? String(field.checked) : field.value;
    return input;
  }

  function actionRequestID() {
    if (typeof crypto.randomUUID === 'function') return crypto.randomUUID();
    const bytes = crypto.getRandomValues(new Uint8Array(16)); bytes[6] = (bytes[6] & 15) | 64; bytes[8] = (bytes[8] & 63) | 128;
    const hex = [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
  }

  function actionPayload(action, body) {
    return request(`${API}/actions/${encodeURIComponent(action.id)}`, { method: 'POST', csrf: csrfToken, body, timeout: 310000 });
  }

  function busyAction(busy, message = '') {
    actionBusy = busy;
    $('action-dialog').setAttribute('aria-busy', String(busy));
    for (const id of ['action-submit', 'action-back', 'close-action', 'action-confirmed']) $(id).disabled = busy;
    $('action-execute').disabled = busy || !$('action-confirmed').checked || !pendingAction;
    $('action-progress').textContent = message;
  }

  async function prepareAction(event) {
    event.preventDefault();
    const action = selectedAction;
    if (!action || actionBusy || !$('action-form').reportValidity()) return;
    const generation = requestGeneration;
    const input = actionInput();
    const requestID = actionRequestID();
    busyAction(true, action.kind === 'mutation' ? 'Preparando a revisão. Nenhuma alteração foi executada.' : 'Consultando o serviço…');
    setActionError(''); $('action-result').hidden = true;
    try {
      const data = await actionPayload(action, { input, phase: action.kind === 'mutation' ? 'preview' : 'execute', request_id: requestID });
      if (generation !== requestGeneration || selectedAction !== action) return;
      if (action.kind !== 'mutation') { renderActionResult(action, data, requestID); return; }
      const expires = Date.parse(data.expires_at);
      if (data.action_id !== action.id || typeof data.confirmation !== 'string' || !data.confirmation || data.confirmation.length > 4096 || !Number.isFinite(expires) || expires <= Date.now()) throw new Error('invalid_preview');
      pendingAction = { input, confirmation: data.confirmation, request_id: requestID, expires };
      $('action-review-summary').textContent = text(data.summary, 'Confirme os dados abaixo antes de executar.');
      $('action-review-values').replaceChildren();
      for (const field of action.fields) {
        const value = input[field.name];
        $('action-review-values').append(fact(fieldLabel(field), field.type === 'password' && value ? 'Definido (não exibido)' : ['boolean', 'checkbox'].includes(field.type) ? value === 'true' ? 'Sim' : 'Não' : value || 'Não informado'));
      }
      $('action-review-expiry').textContent = `Confirmação válida até ${timestamp(data.expires_at)}. A alteração será executada uma única vez.`;
      $('action-form').hidden = true; $('action-review').hidden = false; $('action-confirmed').checked = false;
      actionExpiryTimer = setTimeout(() => { if (!actionBusy && pendingAction) { pendingAction = null; $('action-execute').disabled = true; setActionError('A revisão expirou. Volte aos dados e revise a alteração novamente.'); } }, Math.min(expires - Date.now() + 25, 2147483647));
      announce('Revise os dados e confirme a alteração.');
      $('action-confirmed').focus();
    } catch (failure) { actionFailure(failure, action, requestID, false, generation); }
    finally { if (generation === requestGeneration && selectedAction === action) busyAction(false); }
  }

  function backToAction() {
    if (actionBusy) return;
    clearTimeout(actionExpiryTimer); pendingAction = null;
    $('action-review').hidden = true; $('action-form').hidden = false; $('action-confirmed').checked = false;
    setActionError(''); $('action-submit').focus();
  }

  async function confirmAction() {
    const action = selectedAction; const pending = pendingAction;
    if (!action || !pending || actionBusy || !$('action-confirmed').checked) return;
    if (pending.expires <= Date.now()) { pendingAction = null; $('action-execute').disabled = true; setActionError('A revisão expirou. Volte aos dados e revise novamente.'); return; }
    const generation = requestGeneration;
    clearTimeout(actionExpiryTimer); pendingAction = null;
    busyAction(true, 'Executando a alteração autorizada. Aguarde o resultado; não há repetição automática.');
    setActionError('');
    try {
      const result = await actionPayload(action, { input: pending.input, phase: 'execute', confirmation: pending.confirmation, request_id: pending.request_id });
      if (generation !== requestGeneration || selectedAction !== action) return;
      renderActionResult(action, result, pending.request_id);
    } catch (failure) { actionFailure(failure, action, pending.request_id, true, generation); }
    finally {
      for (const key of Object.keys(pending.input)) delete pending.input[key];
      if (generation === requestGeneration && selectedAction === action) {
        busyAction(false); $('action-review').hidden = true; $('action-form').hidden = true;
        $('action-fields').replaceChildren(); $('action-review-values').replaceChildren();
      }
    }
  }

  function actionFailure(failure, action, requestID, executingMutation, generation) {
    if (generation !== requestGeneration || selectedAction !== action) return;
    if (failure.status === 401 || failure.status === 403) { requireAccess(true); return; }
    const message = failure.status === 409 ? 'A solicitação já foi usada ou o estado mudou. Consulte o estado atual antes de iniciar outra alteração.'
      : failure.status === 400 || failure.status === 422 ? 'O serviço recusou os dados informados. Revise o formulário e as permissões da operação.'
        : executingMutation ? 'Não foi possível confirmar o resultado. A operação não será repetida. Consulte o estado atual antes de iniciar outra alteração.'
          : 'Não foi possível concluir a consulta ou preparar a revisão. Verifique o serviço e tente novamente.';
    setActionError(message);
    if (executingMutation) {
      addOperationHistory(action, 'Resultado não confirmado', requestID);
      $('action-result-request').textContent = `Solicitação: ${requestID}`;
      $('action-result').hidden = false; $('action-result-state').textContent = message;
    }
  }

  function renderActionResult(action, data, requestID) {
    if (!data || data.action_id !== action.id || !['completed', 'failed', 'cancelled', ...(action.kind === 'native_handoff' ? ['handoff'] : []), ...(action.kind === 'information' ? ['information'] : [])].includes(data.status)) throw new Error('invalid_action_result');
    $('action-review').hidden = true;
    const handoff = action.kind === 'native_handoff';
    const outcome = handoff ? 'Orientação para o terminal' : data.status === 'information' ? 'Orientação consultada' : data.status === 'cancelled' ? 'Operação cancelada' : data.status === 'completed' ? action.kind === 'mutation' ? 'Alteração concluída' : 'Consulta concluída' : 'O serviço informou uma falha';
    $('action-result-title').textContent = handoff ? 'Continuar no cliente nativo' : 'Resultado';
    $('action-result-state').textContent = handoff ? 'Esta operação não foi executada pelo painel. Continue no terminal com o cliente nativo e suas permissões.' : outcome;
    const output = $('action-result-output'); output.replaceChildren();
    if (data.truncated === true) {
      const notice = node('p', 'action-output-warning', 'Saída parcial: o limite de exibição foi atingido. O painel não reteve a saída completa deste comando. Não repita uma alteração para recuperar a saída; consulte o estado atual e os registros disponíveis.');
      notice.setAttribute('role', 'status'); output.append(notice);
    }
    if (data.output !== undefined && data.output !== null) {
      const formatted = node('pre', '', typeof data.output === 'string' ? data.output : JSON.stringify(data.output, null, 2));
      formatted.tabIndex = 0; output.append(formatted);
    }
    if (handoff && data.handoff?.executable === 'ivoai' && Array.isArray(data.handoff.args) && data.handoff.args.length <= 32 && data.handoff.args.every((arg) => typeof arg === 'string' && arg.length <= 4096 && !/[\u0000-\u001f\u007f]/.test(arg))) {
      const quoted = ['ivoai', ...data.handoff.args].map((arg) => `'${arg.replaceAll("'", "'\\''")}'`).join(' ');
      output.append(node('p', 'section-note', 'Comando para executar no terminal:'), node('pre', 'native-command', quoted));
    }
    if (!output.childElementCount) output.append(node('p', 'section-note', 'O serviço não retornou informações adicionais.'));
    clearOneTimeCode();
    if (data.status === 'completed' && typeof data.one_time_code === 'string' && data.one_time_code.length > 0 && data.one_time_code.length <= 8192) {
      $('one-time-code').value = data.one_time_code;
      if (data.one_time_code_warning) $('one-time-code-warning').textContent = text(data.one_time_code_warning);
      $('one-time-code-panel').hidden = false;
    }
    $('action-result-request').textContent = `Solicitação: ${requestID}`;
    $('action-result').hidden = false;
    if (action.kind === 'mutation') $('action-form').hidden = true;
    addOperationHistory(action, outcome, requestID);
    announce(outcome);
  }

  function clearOneTimeCode() {
    $('one-time-code').value = ''; $('one-time-code').type = 'password';
    $('one-time-code-panel').hidden = true; $('one-time-code-feedback').textContent = '';
    $('one-time-code-warning').textContent = 'Guarde este código antes de fechar. Ele não será exibido novamente nem salvo no histórico do painel.';
    $('reveal-one-time-code').textContent = 'Mostrar código'; $('reveal-one-time-code').setAttribute('aria-pressed', 'false');
  }

  function addOperationHistory(action, outcome, requestID) {
    operationHistory.unshift({ title: titleForAction(action), outcome, requestID, at: new Date().toISOString() });
    operationHistory = operationHistory.slice(0, 30); renderOperationHistory();
  }

  function renderOperationHistory() {
    $('operation-history').replaceChildren();
    for (const entry of operationHistory) {
      const item = node('li'); const content = node('div');
      content.append(node('p', 'event-title', `${entry.title}: ${entry.outcome}`), node('p', 'event-meta', `${timestamp(entry.at)} / ${entry.requestID}`));
      item.append(node('span', 'event-dot'), content); $('operation-history').append(item);
    }
    $('operation-history-section').hidden = operationHistory.length === 0;
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
      if (overview.schema_version !== 1 || typeof overview.read_only !== 'boolean' || !Array.isArray(sessionData.sessions) || !Array.isArray(components.components) || !Array.isArray(connections.connections)) throw new Error('invalid_response');
      sessions = sessionData.sessions;
      populateFilter('session-state', sessions.map((session) => session.state), stateLabels);
      populateFilter('session-executor', sessions.map((session) => session.executor), executorLabels);
      renderSessions(); renderComponents(components); renderConnections(connections);
      await loadAdministration(generation);
      if (generation !== requestGeneration) return;
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
      await request(localPath('auth/logout'), { method: 'POST', csrf: csrfToken });
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
  for (const id of ['action-search', 'action-group', 'action-kind']) $(id).addEventListener(id === 'action-search' ? 'input' : 'change', renderActions);
  $('action-form').addEventListener('submit', prepareAction);
  $('action-execute').addEventListener('click', confirmAction);
  $('action-confirmed').addEventListener('change', () => { $('action-execute').disabled = !$('action-confirmed').checked || actionBusy || !pendingAction; });
  $('action-back').addEventListener('click', backToAction);
  $('close-action').addEventListener('click', () => { if (!actionBusy) { clearOneTimeCode(); $('action-dialog').close(); } });
  $('action-dialog').addEventListener('cancel', (event) => { if (actionBusy) event.preventDefault(); else clearOneTimeCode(); });
  $('action-dialog').addEventListener('close', () => { clearTimeout(actionExpiryTimer); pendingAction = null; selectedAction = null; $('action-fields').replaceChildren(); $('action-review-values').replaceChildren(); $('action-result-output').replaceChildren(); clearOneTimeCode(); });
  $('reveal-one-time-code').addEventListener('click', () => {
    const visible = $('one-time-code').type === 'password';
    $('one-time-code').type = visible ? 'text' : 'password';
    $('reveal-one-time-code').textContent = visible ? 'Ocultar código' : 'Mostrar código';
    $('reveal-one-time-code').setAttribute('aria-pressed', String(visible));
  });
  $('copy-one-time-code').addEventListener('click', async () => {
    if (!$('one-time-code').value) return;
    try { await navigator.clipboard.writeText($('one-time-code').value); $('one-time-code-feedback').textContent = 'Código copiado. Guarde-o em um local seguro.'; }
    catch { $('one-time-code').type = 'text'; $('one-time-code').focus(); $('one-time-code').select(); $('one-time-code-feedback').textContent = 'Selecione e copie o código neste campo.'; }
  });
  $('clear-operation-history').addEventListener('click', () => { operationHistory = []; renderOperationHistory(); });
  window.addEventListener('hashchange', showView);
  showView();
  request(`${API}/meta`).then((meta) => {
    if (meta.schema_version !== 1 || meta.authentication !== 'oauth_pkce_cookie' || !['local_owner_read_only', 'local_owner', 'owner', 'owner_administration', 'owner_scoped', 'local_owner_administration'].includes(meta.access)) throw new Error('incompatible_contract');
    if (meta.base_path !== undefined && meta.base_path !== baseURL.pathname.replace(/\/$/, '') && meta.base_path !== baseURL.pathname) throw new Error('incompatible_base_path');
    $('sign-in').href = localPath('auth/start');
    return refresh();
  }).catch(() => {
    $('connection-label').textContent = 'Serviço indisponível';
    $('access-title').textContent = 'Serviço indisponível';
    $('access-description').textContent = 'Não foi possível confirmar o contrato do serviço administrativo. Atualize a página depois de verificar o serviço local.';
  });
})();
