// Only the sandbox Pi process imports this; official Node/Pi settings are intact.
import { pathToFileURL } from 'node:url';
import path from 'node:path';
const sdk = process.env.RA2A_PI_SDK;
if (!sdk) throw new Error('sandbox Pi SDK path is required');
const { EnvHttpProxyAgent, setGlobalDispatcher } = await import(
  pathToFileURL(path.join(sdk, 'node_modules/undici/index.js')).href);
setGlobalDispatcher(new EnvHttpProxyAgent());
