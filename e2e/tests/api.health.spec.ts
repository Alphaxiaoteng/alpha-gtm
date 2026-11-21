import { test, expect } from '@playwright/test';

const apiPort = Number(process.env.ALPHA_NEXUS_E2E_API_PORT || 17325);
const apiURL = `http://127.0.0.1:${apiPort}`;

test.describe('API health contract', () => {
  test('GET /api/overview returns core fields', async ({ request }) => {
    const res = await request.get(`${apiURL}/api/overview`);
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body).toHaveProperty('version');
    expect(body).toHaveProperty('data_dir');
    expect(body).toHaveProperty('accounts');
    expect(body).toHaveProperty('platforms');
    expect(typeof body.max_concurrent_spaces === 'number' || body.max_concurrent_spaces === undefined).toBeTruthy();
  });

  test('GET /api/ego/spaces returns the Ego runtime collection', async ({ request }) => {
    const res = await request.get(`${apiURL}/api/ego/spaces`);
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body)).toBeTruthy();
  });

  test('GET /api/accounts returns array', async ({ request }) => {
    const res = await request.get(`${apiURL}/api/accounts`);
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body)).toBeTruthy();
    if (body.length > 0) {
      expect(body[0]).toHaveProperty('id');
      expect(body[0]).toHaveProperty('identityId');
      expect(body[0]).not.toHaveProperty('profileId');
      expect(body[0]).not.toHaveProperty('browserState');
    }
  });
});
