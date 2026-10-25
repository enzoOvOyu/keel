import type {DDMTrends} from '../../lib/fabric-diagnostics'
export const ddmMetrics=['rx_dbm','tx_dbm','temperature_c','voltage_v','bias_ma'] as const
export type DDMMetric=typeof ddmMetrics[number]
export const ddmUnits:Record<DDMMetric,string>={rx_dbm:'dBm',tx_dbm:'dBm',temperature_c:'°C',voltage_v:'V',bias_ma:'mA'}
const colors=['#137cbd','#a45ac7','#c77505','#df4259','#188a67']
export type PlotPoint={x:number;y:number;value:number;connect:boolean}
export function ddmPlot(report:DDMTrends,metrics:DDMMetric[]){
 const left=48,right=740,top=24,bottom=246,start=Date.parse(report.window_start),end=Date.parse(report.window_end),span=Math.max(1,end-start)
 return {left,right,top,bottom,start,end,series:metrics.map(key=>{
  const limits=report.thresholds[key],thresholds=limits?Object.entries(limits).filter((p):p is [string,number]=>typeof p[1]==='number'&&Number.isFinite(p[1])):[]
  const values=report.points.map(p=>p.data?.[key]).filter((v):v is number=>typeof v==='number'&&Number.isFinite(v)),bounds=[...values,...thresholds.map(p=>p[1])]
  let min=bounds.length?Math.min(...bounds):0,max=bounds.length?Math.max(...bounds):1;if(min===max){min-=1;max+=1}const padding=(max-min)*.08;min-=padding;max+=padding
  const y=(v:number)=>bottom-(v-min)/(max-min)*(bottom-top)
  let prior:DDMTrends['points'][number]|undefined
  const points:PlotPoint[]=[]
  for(const p of report.points){const value=p.data?.[key];if(typeof value!=='number'||!Number.isFinite(value)||(p.reasons.includes('ddm_capability_unavailable')||p.reasons.includes('conflicting_same_timestamp_observations'))){prior=undefined;continue}
   const connect=Boolean(prior&&p.continuous_from_previous&&p.module_serial&&p.module_serial===prior.module_serial&&p.provider===prior.provider)
   points.push({x:left+Math.max(0,Math.min(1,(Date.parse(p.at)-start)/span))*(right-left),y:y(value),value,connect});prior=p
  }
  return {key,color:colors[ddmMetrics.indexOf(key)],min,max,points,thresholds:thresholds.map(([kind,value])=>({kind,value,y:y(value)}))}
 })}
}
export async function saveDDMPNG(report:DDMTrends,metrics:DDMMetric[],theme:{background:string;foreground:string;border:string},title:string){
 await document.fonts.ready
 const canvas=document.createElement('canvas');canvas.width=1560;canvas.height=760;const ctx=canvas.getContext('2d');if(!ctx)throw new Error('Canvas unavailable')
 ctx.scale(2,2);ctx.fillStyle=theme.background;ctx.fillRect(0,0,780,380);ctx.fillStyle=theme.foreground;ctx.font='14px "JetBrains Mono", monospace';ctx.fillText(title,16,24)
 const plot=ddmPlot(report,metrics);ctx.save();ctx.translate(0,34);ctx.strokeStyle=theme.border;ctx.beginPath();ctx.moveTo(plot.left,plot.top);ctx.lineTo(plot.left,plot.bottom);ctx.lineTo(plot.right,plot.bottom);ctx.stroke();ctx.font='10px "JetBrains Mono", monospace'
 for(const series of plot.series){ctx.strokeStyle=series.color;ctx.fillStyle=series.color;for(const threshold of series.thresholds){ctx.setLineDash(threshold.kind.startsWith('critical')?[2,3]:[6,4]);ctx.beginPath();ctx.moveTo(plot.left,threshold.y);ctx.lineTo(plot.right,threshold.y);ctx.stroke()};ctx.setLineDash([]);ctx.beginPath();for(const p of series.points){if(p.connect)ctx.lineTo(p.x,p.y);else ctx.moveTo(p.x,p.y)};ctx.stroke();for(const p of series.points){ctx.beginPath();ctx.arc(p.x,p.y,2,0,Math.PI*2);ctx.fill()}}
 ctx.fillStyle=theme.foreground;for(let i=0;i<=4;i++){const at=plot.start+(plot.end-plot.start)*i/4;ctx.fillText(new Date(at).toLocaleTimeString(),plot.left+(plot.right-plot.left)*i/4-28,plot.bottom+18)};ctx.restore()
 plot.series.forEach((series,i)=>{ctx.fillStyle=series.color;ctx.fillText(series.points.length?`${series.key}: ${series.min.toFixed(2)}–${series.max.toFixed(2)} ${ddmUnits[series.key]}`:`${series.key}: unknown / no samples`,16+(i%2)*380,322+Math.floor(i/2)*16)})
 ctx.fillStyle=theme.foreground;ctx.fillText('warn: dashed · crit: dotted · independent metric axes',16,308);ctx.fillText(`source=switch / ${report.evidence_source} · ${report.content_hash.slice(0,20)} · ${report.hours}h · ${report.points.length}/${report.raw_point_count} points`,16,374)
 const blob=await new Promise<Blob>((resolve,reject)=>canvas.toBlob(value=>value?resolve(value):reject(new Error('PNG export failed')),'image/png'))
 const url=URL.createObjectURL(blob),a=document.createElement('a');a.href=url;a.download=`ddm-${report.device_id}-${report.port}-${report.hours}h.png`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)
}
