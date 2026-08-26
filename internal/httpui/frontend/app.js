const byId = id => document.getElementById(id)
const idem = prefix => `${prefix}-${Date.now()}-${crypto.randomUUID()}`

function profileInput() {
  return {
    startMarker: byId('start').value, endMarker: byId('end').value, material: byId('material').value,
	diameterMm: +byId('diameter').value, lengthM: +byId('length').value, volumeM3: +byId('volume').value, targetChlorineMin: +byId('cmin').value, targetChlorineMax: +byId('cmax').value
  }
}

async function request(path, options = {}) {
  const response = await fetch(path, options)
  const data = await response.json().catch(() => ({error: `HTTP ${response.status}`}))
  byId('out').textContent = JSON.stringify(data, null, 2)
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`)
  return data
}

async function createBatch() {
  try {
    const data = await request('/api/batches', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({
      segmentId: byId('segment').value, waterSource: byId('source').value, createdBy: byId('creator').value,
      idempotencyKey: idem('draft-create'), profile: profileInput()
    })})
    byId('bid').value = data.id
    byId('msg').textContent = `已创建 ${data.id}，版本 ${data.version}`
  } catch (error) { byId('msg').textContent = error.message }
}

async function saveDraft() {
  try {
    const current = JSON.parse(byId('out').textContent || '{}')
    const data = await request('/api/batches', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({
      batchId: byId('bid').value, segmentId: byId('segment').value, waterSource: byId('source').value,
      actor: byId('creator').value, expectedVersion: current.version, idempotencyKey: idem('draft-save'), profile: profileInput()
    })})
    byId('msg').textContent = `草稿已保存，版本 ${data.version}`
  } catch (error) { byId('msg').textContent = error.message }
}

async function load(suffix = '') {
	try {
		const data = await request(`/api/batches/${byId('bid').value}${suffix}`)
		if (suffix === '') {
			for (const value of Object.values(templates)) value.expectedVersion = data.version
			if (data.status === 'draft') {
				byId('segment').value = data.segmentId; byId('source').value = data.waterSource; byId('creator').value = data.createdBy
				byId('start').value = data.profile.startMarker; byId('end').value = data.profile.endMarker; byId('material').value = data.profile.material
				byId('diameter').value = data.profile.diameterMm || ''; byId('length').value = data.profile.lengthM || ''; byId('volume').value = data.profile.volumeM3
				byId('cmin').value = data.profile.targetChlorineMin; byId('cmax').value = data.profile.targetChlorineMax
			}
			const released = data.status === 'released'
			for (const id of ['operation', 'templateButton', 'submitButton']) byId(id).disabled = released
			if (released) byId('msg').textContent = '已放行批次仅提供时间线查看与凭据核验'
			else { loadTemplate(); byId('msg').textContent = `已恢复版本 ${data.version} 的操作区` }
		}
		return data
	} catch (error) { byId('msg').textContent = error.message }
}

const templates = {
	freeze: {action: 'precheck', expectedVersion: 1, actor: '现场负责人', idempotencyKey: 'freeze-001', confirmationSummary: '预检返回值；确认时把 action 改为 confirm', warningsConfirmed: false, plan: {flowRateM3h: 20, durationMin: 30, disinfectantTarget: 0.6, samplingPoints: ['P1']}},
  rounds: {expectedVersion: 2, actor: '现场负责人', idempotencyKey: 'round-001', round: {sequence: 1, roundType: 'flush', startedAt: '2026-08-26T01:00:00Z', endedAt: '2026-08-26T01:30:00Z', flowRateM3h: 20, chlorineMgL: 0.6}},
  finish: {expectedVersion: 3, actor: '现场负责人'},
	samples: {expectedVersion: 4, actor: '水质人员', idempotencyKey: 'samples-001', samples: [{samplingPoint: 'P1', witness: '见证人', sampledAt: '2026-08-26T02:00:00Z', turbidityNtu: 0.5, chlorineMgL: 0.6, colonyCfuMl: 10}]},
  corrective: {expectedVersion: 5, actor: '现场负责人', sourceSampleId: 's-id', reason: '余氯偏低', measure: '补充消毒并延长接触时间', affectedPoints: ['P1']},
  reinspect: {expectedVersion: 6, actor: '水质人员', actionId: 'a-id', sample: {samplingPoint: 'P1', witness: '见证人', sampledAt: '2026-08-26T03:00:00Z', turbidityNtu: 0.5, chlorineMgL: 0.6, colonyCfuMl: 10}},
	review: {expectedVersion: 7, actor: '复核人员', reviewToken: '从复核摘要的 reviewEvidence.reviewToken 复制', approved: true, reason: '', targetStage: ''},
  release: {expectedVersion: 8, actor: '复核人员'}
}

function loadTemplate() { byId('payload').value = JSON.stringify(templates[byId('operation').value], null, 2) }
async function submitOperation() {
  try {
    const body = JSON.parse(byId('payload').value)
    await request(`/api/batches/${byId('bid').value}/${byId('operation').value}`, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)})
  } catch (error) { byId('msg').textContent = error.message }
}

byId('createButton').addEventListener('click', createBatch)
byId('saveButton').addEventListener('click', saveDraft)
byId('loadButton').addEventListener('click', () => load())
byId('summaryButton').addEventListener('click', () => load('/summary'))
byId('timelineButton').addEventListener('click', () => load('/timeline'))
byId('verifyButton').addEventListener('click', () => load('/verify'))
byId('templateButton').addEventListener('click', loadTemplate)
byId('submitButton').addEventListener('click', submitOperation)
byId('operation').addEventListener('change', loadTemplate)
let nextCursor = ''
function filterQuery(cursor = '') {
	const params = new URLSearchParams({segmentId: byId('filterSegment').value, waterSource: byId('filterSource').value, status: byId('filterStatus').value, createdBy: byId('filterCreator').value, limit: '20'})
	if (cursor) params.set('cursor', cursor)
	return params.toString()
}
async function search(cursor = '') {
	try {
		const data = await request(`/api/batches?${filterQuery(cursor)}`)
		byId('batchRows').replaceChildren(...data.items.map(item => {
			const row = document.createElement('tr')
			for (const value of [item.id, item.segmentId, `${item.status} / ${item.currentStage}`, item.nextAllowedAction, item.pendingSampleCount, item.openActionCount, new Date(item.updatedAt).toLocaleString()]) { const cell = document.createElement('td'); cell.textContent = value; row.appendChild(cell) }
			row.addEventListener('click', async () => { byId('bid').value = item.id; await load(); if (item.status === 'released') byId('msg').textContent = '已放行批次仅需查看时间线或核验凭据' })
			return row
		}))
		byId('total').textContent = `共 ${data.total} 个批次`; nextCursor = data.nextCursor || ''; byId('nextButton').disabled = !nextCursor
	} catch (error) { byId('msg').textContent = error.message }
}
byId('searchButton').addEventListener('click', () => search())
byId('nextButton').addEventListener('click', () => search(nextCursor))
loadTemplate()
search()
