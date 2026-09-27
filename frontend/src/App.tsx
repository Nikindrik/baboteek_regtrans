import {useEffect,useMemo,useRef,useState} from 'react';
import {Map as MapLibreMap,type GeoJSONSource,type MapLayerMouseEvent} from 'maplibre-gl';
import {AlertTriangle,BusFront,Clock3,Map as MapIcon,Moon,Radio,Search,Sun} from 'lucide-react';
import type {VehicleState} from './types/fleet';
import {useFleetSocket} from './hooks/useFleetSocket';

type ViewMode='fleet'|'map';
type ThemeMode='auto'|'light'|'dark';
type ResolvedTheme='light'|'dark';
type MapFilter='all'|'green'|'yellow'|'red';
type ScenarioKind='reserve'|'traffic-light';
type ScenarioPendingState={kind:ScenarioKind;targetKey:string};
type ScenarioErrorState={targetKey:string;message:string};

type TrafficLightScenario={
  scenario:'traffic_light';
  unit_id:number;
  tr_id:number;
  target_stop_id:number;
  target_time_begin:string;
  extension_s:number;
  baseline_predicted_delay_s:number;
  scenario_predicted_delay_s:number;
  estimated_delay_reduction_s:number;
  baseline_late_probability:number;
  baseline_risk_level:string;
  live_state_changed:boolean;
};

type ReserveScenario={
  scenario:'reserve_vehicle';
  unit_id:number;
  tr_id:number;
  target_stop_id:number;
  target_time_begin:string;
  dispatch_eta_s:number;
  baseline_vehicle_delay_s:number;
  vehicle_delay_after_action_s:number;
  scenario_service_delay_s:number;
  estimated_service_gap_reduction_s:number;
  baseline_late_probability:number;
  baseline_risk_level:string;
  live_state_changed:boolean;
};

type ScenarioResult=TrafficLightScenario|ReserveScenario;

const riskColor={green:'#2f8f5b',yellow:'#bd7b00',red:'#c53f42'} as const;
const themeStorageKey='mgt-dashboard-theme';

const sec=(n:number)=>{
  const abs=Math.abs(Math.round(n));
  const sign=n<0?'-':n>0?'+':'';
  const min=Math.floor(abs/60);
  const seconds=abs%60;
  return min?`${sign}${min}м ${seconds}с`:`${sign}${seconds}с`;
};

const hhmm=(value:string)=>{
  const d=new Date(value);
  if(Number.isNaN(d.getTime())) return '—';
  return d.toLocaleTimeString('ru-RU',{hour:'2-digit',minute:'2-digit'});
};

const validTargetTime=(value:string)=>{
  const ms=Date.parse(value);
  if(!Number.isFinite(ms)) return false;
  return new Date(ms).getUTCFullYear()>1970;
};

const hasValidPrediction=(v:VehicleState)=>
  v.prediction_valid ?? (v.target_stop_id>0&&validTargetTime(v.target_time_begin));

const isContextVehicle=(v:VehicleState)=>v.forecast_eligible===false;
const currentDeviationText=(v:VehicleState)=>isContextVehicle(v)?'—':sec(v.current_delay_s);

const vehicleTargetKey=(v:VehicleState)=>{
  const ms=Date.parse(v.target_time_begin);
  return `${v.target_stop_id}|${Number.isFinite(ms)?ms:0}`;
};

const scenarioTargetKey=(r:ScenarioResult)=>{
  const ms=Date.parse(r.target_time_begin);
  return `${r.target_stop_id}|${Number.isFinite(ms)?ms:0}`;
};

const autoTheme=():ResolvedTheme=>{
  const h=new Date().getHours();
  return h>=7&&h<20?'light':'dark';
};

function FleetMap({vehicles,chosen,onSelect,theme}:{vehicles:VehicleState[];chosen?:VehicleState;onSelect:(id:number)=>void;theme:ResolvedTheme}){
  const containerRef=useRef<HTMLDivElement|null>(null);
  const mapRef=useRef<MapLibreMap|null>(null);
  const vehiclesRef=useRef(vehicles);
  const chosenRef=useRef(chosen);
  const onSelectRef=useRef(onSelect);

  vehiclesRef.current=vehicles;
  chosenRef.current=chosen;
  onSelectRef.current=onSelect;

  const vehicleGeoJSON=()=>({
    type:'FeatureCollection' as const,
    features:vehiclesRef.current
      .filter(v=>v.is_online&&Number.isFinite(v.last_point.lon)&&Number.isFinite(v.last_point.lat))
      .map(v=>({
        type:'Feature' as const,
        geometry:{type:'Point' as const,coordinates:[v.last_point.lon,v.last_point.lat]},
        properties:{
          unit_id:v.unit_id,
          tr_id:String(v.tr_id),
          delay:currentDeviationText(v),
          color:v.is_online&&hasValidPrediction(v)?riskColor[v.risk_level]:'#7b8792',
          radius:chosenRef.current?.unit_id===v.unit_id?10:7,
        },
      })),
  });



  useEffect(()=>{
    if(!containerRef.current) return;

    const map=new MapLibreMap({
      container:containerRef.current,
      style:theme==='dark'
        ?'https://tiles.openfreemap.org/styles/dark'
        :'https://tiles.openfreemap.org/styles/positron',
      center:chosenRef.current
        ?[chosenRef.current.last_point.lon,chosenRef.current.last_point.lat]
        :[37.618423,55.751244],
      zoom:chosenRef.current?14.5:12.7,
      maplibreLogo:false,
    });
    mapRef.current=map;

    map.on('load',()=>{
      map.addSource('fleet',{type:'geojson',data:vehicleGeoJSON()});
      map.addLayer({
        id:'fleet-points',
        type:'circle',
        source:'fleet',
        paint:{
          'circle-radius':['get','radius'],
          'circle-color':['get','color'],
          'circle-stroke-color':theme==='dark'?'#111820':'#ffffff',
          'circle-stroke-width':2,
        },
      });
      map.on('mouseenter','fleet-points',()=>{map.getCanvas().style.cursor='pointer'});
      map.on('mouseleave','fleet-points',()=>{map.getCanvas().style.cursor=''});
      map.on('click','fleet-points',(e:MapLayerMouseEvent)=>{
        const feature=e.features?.[0];
        const id=Number(feature?.properties?.unit_id);
        if(Number.isFinite(id)) onSelectRef.current(id);
      });
    });

    return ()=>{
      map.remove();
      mapRef.current=null;
    };
  },[theme]);

  useEffect(()=>{
    const map=mapRef.current;
    if(!map||!map.isStyleLoaded()) return;
    (map.getSource('fleet') as GeoJSONSource|undefined)?.setData(vehicleGeoJSON());
  },[vehicles,chosen]);

  useEffect(()=>{
    const map=mapRef.current;
    if(!map||!chosen) return;
    map.flyTo({center:[chosen.last_point.lon,chosen.last_point.lat],zoom:14.5,duration:450});
  },[chosen?.unit_id]);

  return <div className="maplibre-host" ref={containerRef}/>;
}

export default function App(){
  const {vehicles,connected}=useFleetSocket();
  const [view,setView]=useState<ViewMode>('fleet');
  const [selected,setSelected]=useState<number>();
  const [mapNotice,setMapNotice]=useState<number>();
  const [filter,setFilter]=useState<MapFilter>('all');
  const [q,setQ]=useState('');
  const [themeMode,setThemeMode]=useState<ThemeMode>(()=>{
    const saved=localStorage.getItem(themeStorageKey);
    return saved==='light'||saved==='dark'||saved==='auto'?saved:'auto';
  });
  const [autoResolved,setAutoResolved]=useState<ResolvedTheme>(()=>autoTheme());
  const [scenario,setScenario]=useState<Record<number,ScenarioResult|undefined>>({});
  const [scenarioPending,setScenarioPending]=useState<Record<number,ScenarioPendingState|undefined>>({});
  const [scenarioError,setScenarioError]=useState<Record<number,ScenarioErrorState|undefined>>({});

  const theme:ResolvedTheme=themeMode==='auto'?autoResolved:themeMode;
  const chosen=vehicles.find(v=>v.unit_id===selected);
  const online=vehicles.filter(v=>v.is_online);
  const offlineCount=vehicles.length-online.length;
  const contextCount=online.filter(isContextVehicle).length;
  const forecastOnline=online.filter(v=>!isContextVehicle(v));
  const predictionReady=forecastOnline.filter(hasValidPrediction);
  const noTargetCount=forecastOnline.length-predictionReady.length;
  const fallbackCount=predictionReady.filter(v=>v.prediction_source==='fallback').length;
  const mlLatencies=predictionReady
    .filter(v=>v.prediction_source!=='fallback'&&typeof v.ml_latency_ms==='number'&&Number.isFinite(v.ml_latency_ms)&&v.ml_latency_ms>0)
    .map(v=>Number(v.ml_latency_ms));
  const avgMLLatency=mlLatencies.length?mlLatencies.reduce((a,b)=>a+b,0)/mlLatencies.length:undefined;
  const red=predictionReady.filter(v=>v.risk_level==='red').length;
  const yellow=predictionReady.filter(v=>v.risk_level==='yellow').length;
  const green=predictionReady.filter(v=>v.risk_level==='green').length;

  useEffect(()=>{
    document.documentElement.dataset.theme=theme;
  },[theme]);

  useEffect(()=>{
    localStorage.setItem(themeStorageKey,themeMode);
  },[themeMode]);

  useEffect(()=>{
    if(themeMode!=='auto') return;
    const update=()=>setAutoResolved(autoTheme());
    update();
    const timer=window.setInterval(update,60_000);
    return ()=>window.clearInterval(timer);
  },[themeMode]);

  const activeIncidents=useMemo(()=>online
    .filter(v=>hasValidPrediction(v)&&(v.risk_level==='yellow'||v.risk_level==='red'))
    .sort((a,b)=>{
      if(a.risk_level!==b.risk_level) return a.risk_level==='red'?-1:1;
      return b.late_probability-a.late_probability;
    }),[online]);

  const normalVehicles=useMemo(()=>{
    const incidentIds=new Set(activeIncidents.map(v=>v.unit_id));
    const query=q.trim().toLowerCase().replace(/^#/,'');
    return vehicles
      .filter(v=>!incidentIds.has(v.unit_id))
      .filter(v=>!query||String(v.unit_id).includes(query)||String(v.tr_id).includes(query)||v.reason.toLowerCase().includes(query))
      .sort((a,b)=>Number(b.is_online)-Number(a.is_online)||a.unit_id-b.unit_id);
  },[vehicles,activeIncidents,q]);

  const mapVehicles=useMemo(
    ()=>online.filter(v=>filter==='all'||(hasValidPrediction(v)&&v.risk_level===filter)),
    [online,filter]
  );

  const cycleTheme=()=>setThemeMode(current=>current==='auto'?'light':current==='light'?'dark':'auto');

  const openVehicleOnMap=(unitId:number)=>{
    setSelected(unitId);
    setMapNotice(unitId);
    setFilter('all');
    setView('map');
  };

  const selectVehicleOnMap=(unitId:number)=>{
    setSelected(unitId);
    setMapNotice(unitId);
  };

  const runScenario=async(unitId:number,kind:ScenarioKind)=>{
    const vehicle=vehicles.find(v=>v.unit_id===unitId);
    if(!vehicle||!hasValidPrediction(vehicle)){
      return;
    }
    const targetKey=vehicleTargetKey(vehicle);
    setScenarioPending(x=>({...x,[unitId]:{kind,targetKey}}));
    setScenarioError(x=>({...x,[unitId]:undefined}));
    try{
      const response=await fetch(`/api/v1/what-if/${kind==='reserve'?'reserve':'traffic-light'}`,{
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body:JSON.stringify(kind==='reserve'
          ?{unit_id:unitId,dispatch_eta_s:300}
          :{unit_id:unitId,extension_s:15}),
      });
      const body=await response.json().catch(()=>({}));
      if(!response.ok){
        const message=response.status===409
          ?'Для этого ТС сейчас нет валидного прогноза на строгом горизонте 10–15 минут.'
          :typeof body?.error==='string'?body.error:'Не удалось выполнить сценарную оценку.';
        throw new Error(message);
      }
      setScenario(x=>({...x,[unitId]:body as ScenarioResult}));
    }catch(error){
      setScenarioError(x=>({...x,[unitId]:{targetKey,message:error instanceof Error?error.message:'Не удалось выполнить сценарную оценку.'}}));
    }finally{
      setScenarioPending(x=>x[unitId]?.targetKey===targetKey?({...x,[unitId]:undefined}):x);
    }
  };

  return <main className="app-shell">
    <header className="topbar">
      <div className="brand-block">
        <div className="brand-kicker">МОСКОВСКИЙ ТРАНСПОРТ</div>
        <h1>Диспетчерская</h1>
      </div>

      <nav className="view-switch" data-view={view} aria-label="Разделы диспетчерской">
        <button type="button" className={view==='fleet'?'active':''} onClick={()=>setView('fleet')}>Транспорт</button>
        <button type="button" className={view==='map'?'active':''} onClick={()=>setView('map')}>Карта</button>
      </nav>

      <div className="topbar-right">
        <div className="system-status" aria-label="Статус системы">
          <span className={'live-state '+(connected?'ok':'warn')}><Radio size={14}/>{connected?'LIVE':'Связь...'}</span>
          <span><b>{online.length}</b> на линии</span>
          <span><b>{contextCount}</b> на мониторинге</span>
          <span><b>{activeIncidents.length}</b> инцидента</span>
          <span>Прогноз <b>10–15 мин</b></span>
          {avgMLLatency!==undefined&&<span>ML <b>{avgMLLatency.toFixed(avgMLLatency<10?1:0)} мс</b></span>}
          {fallbackCount>0&&<span className="degraded-state">Резерв <b>{fallbackCount}</b></span>}
        </div>
        <button className="theme-button" type="button" onClick={cycleTheme} title="Сменить тему" aria-label="Сменить тему">
          {themeMode==='auto'?<Clock3 size={16}/>:theme==='light'?<Sun size={16}/>:<Moon size={16}/>}<span>{themeMode==='auto'?'Авто':theme==='light'?'Светлая':'Тёмная'}</span>
        </button>
      </div>
    </header>

    <div key={view} className={'view-stage view-'+view}>
      {view==='fleet'?<section className="transport-board">
        <section className="fleet-panel normal-panel">
          <div className="panel-head">
            <div>
              <span className="section-kicker">ТРАНСПОРТ</span>
              <h2>ТС без активных инцидентов</h2>
              <p>{green} в графике{contextCount?` · ${contextCount} на мониторинге`:''}{noTargetCount?` · ${noTargetCount} без цели 10–15 мин`:''}{offlineCount?` · ${offlineCount} без связи`:''}</p>
            </div>
            <label className="fleet-search"><Search size={16}/><input value={q} onChange={e=>setQ(e.target.value)} placeholder="ТС, рейс или причина"/></label>
          </div>

          <div className="vehicle-list-head" aria-hidden="true">
            <span>Статус</span><span>ТС / рейс</span><span>Скорость</span><span>Сейчас</span><span>10–15 мин</span><span>Вероятность</span>
          </div>
          <div className="vehicle-list">
            {normalVehicles.map(v=><VehicleRow key={v.unit_id} vehicle={v} selected={selected===v.unit_id} onSelect={()=>openVehicleOnMap(v.unit_id)}/>)}
            {!normalVehicles.length&&<div className="empty-state">Ничего не найдено.</div>}
          </div>
        </section>

        <section className="fleet-panel incident-panel">
          <div className="panel-head incident-head">
            <div>
              <span className="section-kicker">ТРЕБУЮТ ВНИМАНИЯ</span>
              <h2>Инцидентные ТС</h2>
              <p>{red} критических · {yellow} с риском</p>
            </div>
            <div className={'incident-count '+(red?'critical':'')}>{activeIncidents.length}</div>
          </div>

          <div className="incident-list">
            {activeIncidents.map(v=><IncidentItem
              key={v.unit_id}
              vehicle={v}
              selected={selected===v.unit_id}
              onSelect={()=>openVehicleOnMap(v.unit_id)}
              onScenario={kind=>runScenario(v.unit_id,kind)}
              pending={scenarioPending[v.unit_id]?.targetKey===vehicleTargetKey(v)?scenarioPending[v.unit_id]?.kind:undefined}
              result={scenario[v.unit_id]&&scenarioTargetKey(scenario[v.unit_id]!)===vehicleTargetKey(v)?scenario[v.unit_id]:undefined}
              error={scenarioError[v.unit_id]?.targetKey===vehicleTargetKey(v)?scenarioError[v.unit_id]?.message:undefined}
            />)}
            {!activeIncidents.length&&<div className="all-clear"><span className="status-dot green"/>Активных инцидентов нет</div>}
          </div>
        </section>
      </section>:<section className="map-page">
        <div className="map-topline">
          <div className="map-filters">
            {(['all','green','yellow','red'] as const).map(x=><button type="button" className={filter===x?'active':''} onClick={()=>setFilter(x)} key={x}>{x==='all'?'Все':x==='green'?'В графике':x==='yellow'?'Риск':'Критические'}</button>)}
          </div>
          <div className="map-context">
            <span>{mapVehicles.length} ТС на карте</span>
          </div>
        </div>
        <div className="map-frame">
          <FleetMap vehicles={mapVehicles} chosen={chosen} onSelect={selectVehicleOnMap} theme={theme}/>
          {mapNotice&&chosen&&mapNotice===chosen.unit_id&&<button
            type="button"
            className="map-selection-notice"
            onClick={()=>setMapNotice(undefined)}
            title="Нажмите, чтобы скрыть"
            aria-label={`Скрыть карточку выбранного ТС ${chosen.unit_id}`}
          >
            <span className={'status-dot '+(!chosen.is_online||!hasValidPrediction(chosen)?'offline':chosen.risk_level)}/>
            <span className="map-selection-copy">
              <b>ТС #{chosen.unit_id}</b>
              <small>рейс {chosen.tr_id}</small>
            </span>
            <span className="map-selection-dismiss">Скрыть</span>
          </button>}
          <div className="map-legend"><span><i className="green"/>В графике</span><span><i className="yellow"/>Риск</span><span><i className="red"/>Критический</span><span><i style={{background:'var(--offline)'}}/>Мониторинг / без цели</span></div>
        </div>
      </section>}
    </div>
  </main>;
}

function VehicleRow({vehicle,selected,onSelect}:{vehicle:VehicleState;selected:boolean;onSelect:()=>void}){
  const predictionValid=hasValidPrediction(vehicle);
  const context=isContextVehicle(vehicle);
  const status=!vehicle.is_online||context||!predictionValid?'offline':vehicle.risk_level;
  const label=!vehicle.is_online?'Нет связи':context?'Мониторинг':!predictionValid?'Нет цели 10–15 мин':vehicle.risk_level==='green'?'В графике':vehicle.risk_level==='yellow'?'Риск':'Критический';
  return <button type="button" className={'vehicle-row '+(selected?'selected':'')} onClick={onSelect}>
    <span className="row-status"><i className={'status-dot '+status}/>{label}</span>
    <span className="vehicle-id"><b>ТС #{vehicle.unit_id}</b><small>рейс {vehicle.tr_id}{vehicle.prediction_source==='fallback'?' · резервный прогноз':''}</small></span>
    <span>{Math.round(vehicle.last_point.speed)} км/ч</span>
    <span>{currentDeviationText(vehicle)}</span>
    <span className={predictionValid&&vehicle.predicted_delay_s>120?'danger-text':''}>{predictionValid?sec(vehicle.predicted_delay_s):'—'}</span>
    <span>{predictionValid?`${Math.round(vehicle.late_probability*100)}%`:'—'}</span>
  </button>;
}

function IncidentItem({vehicle,selected,onSelect,onScenario,pending,result,error}:{
  vehicle:VehicleState;
  selected:boolean;
  onSelect:()=>void;
  onScenario:(kind:ScenarioKind)=>void;
  pending?:ScenarioKind;
  result?:ScenarioResult;
  error?:string;
}){
  return <article className={'incident-item '+vehicle.risk_level+(selected?' selected':'')}>
    <button type="button" className="incident-main" onClick={onSelect}>
      <div className="incident-title-row">
        <span className={'risk-label '+vehicle.risk_level}><AlertTriangle size={14}/>{vehicle.risk_level==='red'?'Критический':'Риск'}</span>
        {vehicle.prediction_source==='fallback'&&<span className="fallback-badge">Резервный прогноз</span>}
        <time>обновлено {hhmm(vehicle.updated_at)}</time>
      </div>
      <div className="incident-name"><b>ТС #{vehicle.unit_id}</b><span>рейс {vehicle.tr_id}</span><span>вероятность {Math.round(vehicle.late_probability*100)}%</span></div>
      <p className="incident-reason">{vehicle.reason}</p>
      <div className="incident-metrics">
        <div><span>Сейчас</span><b>{sec(vehicle.current_delay_s)}</b></div>
        <div><span>Через 10–15 мин</span><b className="danger-text">{sec(vehicle.predicted_delay_s)}</b></div>
        <div><span>Целевая остановка</span><b>{vehicle.target_stop_id||'—'}</b><small>{vehicle.target_time_begin?hhmm(vehicle.target_time_begin):'—'}</small></div>
      </div>
    </button>

    <div className="incident-actions">
      <button type="button" onClick={()=>onScenario('reserve')} disabled={Boolean(pending)}><BusFront size={15}/>{pending==='reserve'?'Расчёт…':'Резервное ТС'}</button>
      <button type="button" onClick={()=>onScenario('traffic-light')} disabled={Boolean(pending)}><MapIcon size={15}/>{pending==='traffic-light'?'Расчёт…':'Приоритет светофора +15 с'}</button>
    </div>

    {result&&<ScenarioSummary result={result}/>} 
    {error&&<div className="scenario-error">{error}</div>}
  </article>;
}

function ScenarioSummary({result}:{result:ScenarioResult}){
  if(result.scenario==='traffic_light'){
    return <div className="scenario-result">
      <div className="scenario-caption">Сценарная оценка · live-прогноз не меняется</div>
      <div className="scenario-grid">
        <div><span>Текущий прогноз</span><b>{sec(result.baseline_predicted_delay_s)}</b></div>
        <div><span>Возможный выигрыш</span><b>до {Math.round(result.estimated_delay_reduction_s)} с</b></div>
        <div><span>Оценка после меры</span><b>{sec(result.scenario_predicted_delay_s)}</b></div>
      </div>
    </div>;
  }
  return <div className="scenario-result">
    <div className="scenario-caption">Сценарная оценка · live-прогноз не меняется</div>
    <div className="scenario-grid">
      <div><span>Текущая задержка ТС</span><b>{sec(result.baseline_vehicle_delay_s)}</b></div>
      <div><span>Резерв готов через</span><b>{sec(result.dispatch_eta_s).replace('+','')}</b></div>
      <div><span>Сокращение разрыва</span><b>{sec(result.estimated_service_gap_reduction_s).replace('+','')}</b></div>
    </div>
  </div>;
}
