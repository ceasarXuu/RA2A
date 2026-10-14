"""Private, minimal real-credential projections; never serialize private_env."""
import hashlib
import json
import os
from pathlib import Path
import tomllib
from urllib.parse import urlsplit


class BlockedAuth(ValueError):
    pass


def _read(path, snapshots):
    if path.is_symlink() or not path.is_file():
        raise BlockedAuth('required configuration is absent or a symbolic link')
    raw = path.read_bytes()
    snapshots[path] = hashlib.sha256(raw).hexdigest()
    return tomllib.loads(raw.decode()) if path.suffix == '.toml' else json.loads(raw)


def _dir(path):
    missing, parent = [], path
    while not parent.exists():
        missing.append(parent)
        parent = parent.parent
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    if path.is_symlink() or path.stat().st_uid != os.getuid():
        raise BlockedAuth('private profile directory ownership is invalid')
    for created in {path, *missing}:
        created.chmod(0o700)


def _write(path, data):
    _dir(path.parent)
    # Exclusivity prevents accidentally refreshing or modifying an earlier profile.
    with path.open('x', encoding='utf-8') as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(data if isinstance(data, str) else json.dumps(data))


def _credential(entry):
    if not isinstance(entry, dict):
        raise BlockedAuth('credential entry is invalid')
    if entry.get('type') in ('api', 'api_key') and isinstance(entry.get('key'), str) and entry['key']:
        return {'type': entry['type'], 'key': entry['key']}
    if entry.get('type') == 'oauth' and all(entry.get(k) for k in ('access', 'refresh')):
        return {k: entry[k] for k in ('type', 'access', 'refresh', 'expires', 'accountId') if k in entry}
    raise BlockedAuth('selected provider credential type is unsupported')


def _bin(home, kind):
    if kind == 'codex':
        candidates = [home / '.codex/packages/standalone/current/bin/codex',
                      home / '.codex/packages/app-server-daemon/current/bin/codex']
    elif kind == 'opencode':
        candidates = [home / '.opencode/bin/opencode']
    else:
        candidates = sorted((home / '.nvm/versions/node').glob('*/bin/pi'), reverse=True)
    for path in candidates:
        if path.is_file():
            resolved = path.resolve()
            if kind == 'pi':
                node = path.parent / 'node'
                if node.is_file():
                    return {'executable': str(node.resolve()), 'args': [str(resolved)],
                            'sdk': str(resolved.parents[2])}
            elif os.access(resolved, os.X_OK):
                return {'executable': str(resolved), 'args': []}
    raise BlockedAuth('native client binary is unavailable')


def _codex(home, target, snapshots, private_env):
    config = _read(home / '.codex/config.toml', snapshots)
    provider, model = config.get('model_provider', 'openai'), config.get('model')
    if provider != 'openai' or not isinstance(model, str) or not model:
        raise BlockedAuth('explicit official OpenAI model selection is required')
    auth = _read(home / '.codex/auth.json', snapshots)
    if auth.get('auth_mode') == 'chatgpt' and isinstance(auth.get('tokens'), dict):
        tokens = auth['tokens']
        if not all(tokens.get(k) for k in ('access_token', 'refresh_token', 'id_token')):
            raise BlockedAuth('ChatGPT authentication is incomplete')
        auth = {k: auth[k] for k in ('auth_mode', 'tokens', 'last_refresh') if k in auth}
    elif auth.get('auth_mode') == 'apikey' and auth.get('OPENAI_API_KEY'):
        private_env['OPENAI_API_KEY'] = auth['OPENAI_API_KEY']
        auth = {'auth_mode': 'apikey', 'OPENAI_API_KEY': auth['OPENAI_API_KEY']}
    else:
        raise BlockedAuth('Codex authentication mode is unsupported')
    _write(target / '.codex/auth.json', auth)
    effort = config.get('model_reasoning_effort')
    if effort is not None and effort not in ('none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'):
        raise BlockedAuth('Codex reasoning effort is unsupported')
    _write(target / '.codex/config.toml', 'model = ' + json.dumps(model) + '\n'
           + ('model_reasoning_effort = '+json.dumps(effort)+'\n' if effort is not None else '') +
           'approval_policy = "never"\nsandbox_mode = "workspace-write"\n[features]\nplugins = false\n')
    return {'provider': provider, 'model': model,
            'reasoning_effort': effort, 'hosts': ['chatgpt.com', 'auth.openai.com', 'api.openai.com']}


def _opencode(home, target, snapshots, private_env, model_override=None, effort_override=None):
    config = _read(home / '.config/opencode/opencode.json', snapshots)
    if model_override is not None and (not isinstance(model_override, str) or
            len(model_override) > 200 or '/' not in model_override or
            not all(model_override.split('/', 1)) or any(c.isspace() for c in model_override)):
        raise BlockedAuth('explicit OpenCode model must use provider/model format')
    selected = model_override if model_override is not None else config.get('model')
    provenance = 'sandbox-explicit-override' if model_override is not None else 'config.model'
    catalog = None
    if not isinstance(selected, str) or '/' not in selected:
        # Fixed v1.18.34 Provider.defaultModel reads the first available recent.
        recent = _read(home / '.local/state/opencode/model.json', snapshots).get('recent', [])
        catalog = _read(home / '.cache/opencode/models.json', snapshots)
        entries = [x for x in recent if isinstance(x, dict) and
                   x.get('modelID') in catalog.get(x.get('providerID'), {}).get('models', {})]
        if not entries:
            raise BlockedAuth('OpenCode native default cannot be resolved from recent and model catalog')
        selected = entries[0]['providerID'] + '/' + entries[0]['modelID']
        provenance = 'native-v1.18.34-defaultModel-recent'
    provider, model = selected.split('/', 1)
    custom = config.get('provider', {}).get(provider)
    if custom is not None:
        raise BlockedAuth('custom OpenCode provider projection requires an explicit safe schema')
    auth = _read(home / '.local/share/opencode/auth.json', snapshots)
    credential = _credential(auth.get(provider))
    catalog = catalog or _read(home / '.cache/opencode/models.json', snapshots)
    description = catalog.get(provider, {})
    selected_model = description.get('models', {}).get(model, {})
    if effort_override is not None:
        efforts = [v for item in selected_model.get('reasoning_options', [])
                   if item.get('type') == 'effort' for v in item.get('values', [])]
        if effort_override not in efforts:
            raise BlockedAuth('explicit OpenCode reasoning effort is unsupported by the selected model')
    private_config = {'model': selected, 'mcp': {}, 'permission': {'*': 'deny'}}
    if effort_override is not None:
        private_config['provider'] = {provider: {'models': {model: {'options': {'reasoningEffort': effort_override}}}}}
    _write(target / '.local/share/opencode/auth.json', {provider: credential})
    _write(target / '.config/opencode/opencode.json', private_config)
    hostname = urlsplit(description.get('api', '')).hostname
    # Fixed 1.18.34 bundled OpenAI OAuth plugin rewrites SDK requests to
    # chatgpt.com and refreshes only this private auth copy at auth.openai.com.
    oauth_openai = (provider == 'openai' and credential['type'] == 'oauth'
                    and description.get('npm') == '@ai-sdk/openai' and not hostname)
    if (not hostname and not oauth_openai) or model not in description.get('models', {}):
        raise BlockedAuth('OpenCode selected model provider endpoint is unverified')
    hosts = ['chatgpt.com', 'auth.openai.com', 'api.openai.com'] if oauth_openai else [hostname]
    _write(target / '.cache/opencode/models.json', {provider: {
        **{k: description[k] for k in ('id', 'env', 'npm', 'api', 'name', 'doc') if k in description},
        'models': {model: description['models'][model]}}})
    return {'provider': provider, 'model': model, 'selection_source': provenance,
            'reasoning_effort': effort_override, 'hosts': hosts}


def _pi(home, target, snapshots, private_env):
    settings = _read(home / '.pi/agent/settings.json', snapshots)
    provider, model = settings.get('defaultProvider'), settings.get('defaultModel')
    if not isinstance(provider, str) or not isinstance(model, str):
        raise BlockedAuth('Pi explicit provider and model selection is required')
    auth = _read(home / '.pi/agent/auth.json', snapshots)
    credential = _credential(auth.get(provider))
    store = _read(home / '.pi/agent/models-store.json', snapshots)
    records = store.get(provider, {}).get('models', [])
    selected = [item for item in records if item.get('id') == model]
    if len(selected) != 1 or selected[0].get('provider') != provider:
        raise BlockedAuth('Pi selected model is not unambiguous in native model store')
    allowed = ('id', 'name', 'api', 'provider', 'baseUrl', 'reasoning', 'input', 'cost',
               'contextWindow', 'maxTokens', 'compat', 'thinkingLevelMap', 'inputLimits', 'type')
    record = {k: selected[0][k] for k in allowed if k in selected[0]}
    _write(target / '.pi/agent/auth.json', {provider: credential})
    _write(target / '.pi/agent/settings.json', {'defaultProvider': provider, 'defaultModel': model})
    catalog = {k: store[provider][k] for k in ('checkedAt', 'lastModified', 'etag') if k in store[provider]}
    _write(target / '.pi/agent/models-store.json', {provider: {**catalog, 'models': [record]}})
    hostname = urlsplit(record.get('baseUrl', '')).hostname
    if not hostname:
        raise BlockedAuth('Pi selected provider endpoint is unverified')
    return {'provider': provider, 'model': model, 'hosts': [hostname, 'pi.dev']}


def prepare(root: Path, selected: list[str], host_home: Path, *, opencode_model=None, opencode_effort=None) -> dict:
    """Return public manifest, nodes/bins, and per-kind PRIVATE env; no model calls.

    BLOCKED_AUTH is per kind. Consumers must skip blocked nodes, never choose a
    different provider. All node homes are mounted as /home/sandbox. Source hashes
    are verified on exit; private credentials are exclusively created, not linked.
    """
    root, host_home = Path(root).absolute(), Path(host_home).absolute()
    if root == host_home or root in host_home.parents:
        raise BlockedAuth('sandbox root must differ from the official home')
    if any(p.is_symlink() for p in (root, *root.parents)):
        raise BlockedAuth('sandbox root has a symbolic link ancestor')
    _dir(root)
    if any(root.iterdir()):
        raise BlockedAuth('sandbox credential root must be fresh and empty')
    result = {'manifest': {}, 'nodes': {}, 'private_env': {}, 'bins': {}}
    snapshots = {}
    for kind in selected:
        if kind not in ('codex', 'opencode', 'pi') or kind in result['manifest']:
            raise ValueError('unknown or duplicate client kind')
        target = root / kind / 'home'
        _dir(target)
        private_env = {}
        try:
            binary = _bin(host_home, kind)
            choice = (_opencode(host_home, target, snapshots, private_env, opencode_model, opencode_effort)
                      if kind == 'opencode' else
                      globals()['_' + kind](host_home, target, snapshots, private_env))
            result['manifest'][kind] = {'status': 'READY', **choice}
            result['bins'][kind] = binary['args'][0] if kind == 'pi' else binary['executable']
            if kind == 'pi':
                result['bins']['node'] = binary['executable']
                result['bins']['pi_sdk'] = binary['sdk']
            result['nodes'][kind] = {'home': str(target), 'inside_home': '/home/sandbox'}
            result['private_env'][kind] = private_env
        except (BlockedAuth, OSError, ValueError, KeyError) as error:
            # Exception text may contain config data, so expose only fixed reasons.
            result['manifest'][kind] = {'status': 'BLOCKED_AUTH',
                                       'reason': str(error) if isinstance(error, BlockedAuth) else
                                       'configuration or private projection validation failed'}
    unchanged = all(path.is_file() and hashlib.sha256(path.read_bytes()).hexdigest() == digest
                    for path, digest in snapshots.items())
    result['manifest']['source_auth_unchanged'] = unchanged
    if not unchanged:
        raise BlockedAuth('official authentication/configuration changed during projection')
    return result
