-- 005_analysis.sql — the product: rubric-driven structured assessment.
--
-- The shape here follows one rule that everything else falls out of: the model
-- fills in a rubric, it does not produce a score. Criteria and weights live in
-- `analysis_domains`; the model writes evidence and a per-criterion rating into
-- `assessments.findings`; the overall number is arithmetic performed by Go over
-- those two. Nothing anywhere stores "the score the model said".
--
-- That is what makes a rejection defensible. "68/100" on its own is an opinion;
-- "68/100, because criterion `traction` scored 2 of 5 on this quoted evidence,
-- at weight 0.20" is a finding somebody can argue with.

-- Domains (rubrics) -----------------------------------------------------
CREATE TABLE IF NOT EXISTS analysis_domains (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',

    -- Bumped by hand whenever criteria change. Assessments snapshot it (see
    -- below), so this is what lets an old report still be read against the
    -- rubric it was actually produced under.
    version     INTEGER NOT NULL DEFAULT 1,

    -- Array of criterion objects:
    --   {key, label, description, weight, guidance, scale_max}
    --
    -- JSONB rather than a `criteria` table with a foreign key. The set is read
    -- whole on every assessment and never queried across domains — there is no
    -- "find every rubric containing a criterion named X" question — so a join
    -- would buy nothing and cost a second round trip on the hot path. It is
    -- also the thing that gets snapshotted, and snapshotting a document is
    -- trivial where snapshotting a row set is a versioning subsystem.
    criteria    JSONB NOT NULL DEFAULT '[]',

    -- Domain-specific instruction prepended to the analysis prompt.
    guidance    TEXT NOT NULL DEFAULT '',

    -- The PEFT build tuned for this domain's output schema, if one exists.
    -- NULL means the base model, which is the honest state until a build has
    -- been measured to beat it.
    adapter_id  UUID REFERENCES llm_adapters(id) ON DELETE SET NULL,

    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_analysis_domains_active ON analysis_domains(is_active);

-- Assessments -----------------------------------------------------------
CREATE TABLE IF NOT EXISTS assessments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- ON DELETE RESTRICT, unlike most FKs here: deleting a rubric that reports
    -- were produced under would orphan the very audit trail this table exists
    -- to be. A domain that is no longer wanted gets is_active = false.
    domain_id      UUID NOT NULL REFERENCES analysis_domains(id) ON DELETE RESTRICT,

    -- The rubric as it stood at assessment time, copied not referenced.
    -- Deliberate duplication: an operator who tunes a weight next month must
    -- not silently change what last month's report meant. Without this, every
    -- past assessment becomes unexplainable the first time the rubric moves.
    domain_version    INTEGER NOT NULL,
    criteria_snapshot JSONB NOT NULL,

    subject_title  TEXT NOT NULL DEFAULT '',
    subject        TEXT NOT NULL,

    -- Array of finding objects:
    --   {key, score, evidence[], rationale, evidence_found}
    findings       JSONB NOT NULL DEFAULT '[]',

    -- Weighted mean over criteria that had evidence, renormalised by their
    -- weights. NULL when nothing could be assessed at all.
    overall_score  DOUBLE PRECISION,

    -- Share of total rubric weight that had any evidence behind it. This is
    -- reported next to the score rather than folded into it, because they mean
    -- different things: a 75 at 0.9 coverage is a good case, a 75 at 0.3
    -- coverage is a thin deck that happens to be strong where it speaks. An
    -- accelerator wanting to know what applicants leave out reads this column.
    coverage       DOUBLE PRECISION NOT NULL DEFAULT 0,

    -- Whether the model's output parsed against the domain schema on its own.
    -- The measurement the entire product plan rests on: if a 2B model cannot
    -- fill this schema reliably, the LoRA work is what closes the gap, and this
    -- column is how the before/after is proven rather than asserted.
    schema_valid   BOOLEAN NOT NULL DEFAULT false,
    repair_attempts INTEGER NOT NULL DEFAULT 0,

    -- Groups repeat runs of one subject so consistency can be measured. NULL
    -- for ordinary assessments. A separate trials table would duplicate every
    -- column here to record the same thing.
    trial_group    UUID,

    model          TEXT NOT NULL DEFAULT '',
    target         TEXT NOT NULL DEFAULT 'server',
    adapter_id     UUID REFERENCES llm_adapters(id) ON DELETE SET NULL,
    latency_ms     INTEGER NOT NULL DEFAULT 0,
    prompt_tokens  INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,

    -- Kept verbatim for audit and for debugging schema failures. It is the only
    -- way to tell "the model wrote nonsense" from "our parser is wrong", and
    -- after a bad report reaches a customer that distinction is the whole
    -- investigation.
    raw_response   TEXT NOT NULL DEFAULT '',

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_assessments_user ON assessments(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_assessments_domain ON assessments(domain_id, created_at DESC);
-- Partial: trial groups are a small minority of rows, so indexing only the
-- non-NULL ones keeps this index a fraction of the size of a full one.
CREATE INDEX IF NOT EXISTS idx_assessments_trial ON assessments(trial_group)
    WHERE trial_group IS NOT NULL;

ALTER TABLE assessments DROP CONSTRAINT IF EXISTS assessments_ranges_check;
ALTER TABLE assessments ADD CONSTRAINT assessments_ranges_check CHECK (
    (overall_score IS NULL OR (overall_score >= 0 AND overall_score <= 100))
    AND coverage >= 0 AND coverage <= 1
);

-- Seed rubrics ----------------------------------------------------------
--
-- ON CONFLICT DO NOTHING, not DO UPDATE: these are starting points an operator
-- is expected to edit, and a migration that runs on every boot must not undo
-- their edits. Changing the shipped defaults later means a new slug or a
-- deliberate version bump, not overwriting somebody's tuned weights.
--
-- Weights sum to 1.0 in each rubric. Not enforced by a constraint because the
-- scoring renormalises anyway (it has to, since criteria without evidence drop
-- out) — so a rubric summing to 0.9 is merely unusual, not broken.

INSERT INTO analysis_domains (slug, name, description, guidance, criteria) VALUES (
    'startup-investability',
    'Startup Yatırım Yapılabilirliği',
    'Bir girişimin yatırım açısından değerlendirilmesi: problem, pazar, çekiş, ekip ve finansal tutarlılık.',
    'Sen bir yatırım analistisin. Görevin karar vermek DEĞİL, kanıt toplamak. '
    'Her kriter için sadece verilen metinde geçen bilgiye dayan. '
    'Metinde o kritere dair bilgi yoksa evidence_found=false yaz ve score alanını null bırak — '
    'bilgi yokluğunu düşük puan olarak yorumlama. '
    'Her değerlendirmeyi metinden birebir alıntıyla gerekçelendir.',
    '[
      {"key":"problem_clarity","label":"Problem tanımı","weight":0.10,"scale_max":5,
       "description":"Çözülen problem net, gerçek ve acil mi?",
       "guidance":"Kimin hangi acısı? Ne sıklıkta yaşanıyor? Bugün nasıl çözülüyor?"},
      {"key":"market_size","label":"Pazar büyüklüğü","weight":0.15,"scale_max":5,
       "description":"Erişilebilir pazar anlamlı ve gerekçelendirilmiş mi?",
       "guidance":"TAM/SAM/SOM ayrımı yapılmış mı, sayılar kaynaklı mı, yoksa tepeden mi indirilmiş?"},
      {"key":"solution_differentiation","label":"Çözüm farkı","weight":0.15,"scale_max":5,
       "description":"Çözüm rakiplerden savunulabilir şekilde ayrışıyor mu?",
       "guidance":"Teknoloji, veri, ağ etkisi veya dağıtım avantajı var mı? Kopyalanma süresi ne?"},
      {"key":"traction","label":"Çekiş","weight":0.20,"scale_max":5,
       "description":"Kanıtlanmış kullanıcı, gelir veya büyüme var mı?",
       "guidance":"Mutlak sayı ve büyüme oranı birlikte aranır. Pilot ve niyet mektubu çekiş değildir."},
      {"key":"business_model","label":"İş modeli","weight":0.12,"scale_max":5,
       "description":"Gelir modeli ve birim ekonomisi tutarlı mı?",
       "guidance":"CAC, LTV, brüt marj. Birim ekonomisi negatifse ölçeğin bunu nasıl çözdüğü açıklanmış mı?"},
      {"key":"team","label":"Ekip","weight":0.15,"scale_max":5,
       "description":"Ekip bu problemi çözmek için doğru ekip mi?",
       "guidance":"Alan tecrübesi, tamamlayıcılık, tam zamanlılık, birlikte çalışma geçmişi."},
      {"key":"competition","label":"Rekabet","weight":0.05,"scale_max":5,
       "description":"Rekabet gerçekçi haritalanmış mı?",
       "guidance":"\"Rakibimiz yok\" bir zayıflık işaretidir. Dolaylı rakipler ve statüko sayılır."},
      {"key":"financials_ask","label":"Finansal plan ve talep","weight":0.05,"scale_max":5,
       "description":"İstenen tutar, kullanım planı ve projeksiyon tutarlı mı?",
       "guidance":"Talep edilen tutar hangi kilometre taşına kadar yetiyor? Runway hesabı var mı?"},
      {"key":"risk","label":"Risk farkındalığı","weight":0.03,"scale_max":5,
       "description":"Ana riskler tanımlanmış ve azaltma planı var mı?",
       "guidance":"Riski hiç konuşmayan sunum, riski olmayan girişim değil, farkında olmayan ekiptir."}
    ]'::jsonb
) ON CONFLICT (slug) DO NOTHING;

INSERT INTO analysis_domains (slug, name, description, guidance, criteria) VALUES (
    'digital-marketing',
    'Dijital Pazarlama Analizi ve Platform Seçimi',
    'Bir marka/ürün için kanal karması ve platform seçiminin değerlendirilmesi.',
    'Sen bir dijital pazarlama stratejistisin. Görevin kampanya yazmak DEĞİL, '
    'verilen brief''i kriterlere göre değerlendirmek ve platform önerisini gerekçelendirmek. '
    'Metinde bilgi yoksa evidence_found=false yaz ve score alanını null bırak. '
    'Her önerini brief''ten birebir alıntıyla bağla.',
    '[
      {"key":"audience_clarity","label":"Hedef kitle netliği","weight":0.20,"scale_max":5,
       "description":"Hedef kitle tanımlı, dar ve ulaşılabilir mi?",
       "guidance":"Demografi tek başına yetmez; davranış, niyet ve bulunduğu mecra aranır."},
      {"key":"channel_fit","label":"Kanal uyumu","weight":0.25,"scale_max":5,
       "description":"Seçilen platformlar kitleye ve ürüne uygun mu?",
       "guidance":"Kitlenin gerçekte bulunduğu yer ile seçilen kanal örtüşüyor mu? Satın alma döngüsü uzunluğu kanalı belirler."},
      {"key":"budget_realism","label":"Bütçe gerçekçiliği","weight":0.15,"scale_max":5,
       "description":"Bütçe hedeflerle ve kanal maliyetleriyle tutarlı mı?",
       "guidance":"Hedef edinme sayısı × tahmini CAC, verilen bütçeyi aşıyor mu?"},
      {"key":"differentiation","label":"Mesaj farkı","weight":0.15,"scale_max":5,
       "description":"Konumlandırma ve mesaj rakiplerden ayrışıyor mu?",
       "guidance":"Mesaj rakibin sitesine konsa fark edilir mi? Edilmiyorsa ayrışma yoktur."},
      {"key":"measurement_plan","label":"Ölçüm planı","weight":0.15,"scale_max":5,
       "description":"Başarı nasıl ölçülecek, tanımlı mı?",
       "guidance":"Kuzey yıldızı metrik, atıf modeli, ölçüm aracı. Gösterge metrikler (beğeni) sayılmaz."},
      {"key":"competitive_context","label":"Rekabet bağlamı","weight":0.10,"scale_max":5,
       "description":"Rakiplerin kanal davranışı dikkate alınmış mı?",
       "guidance":"Rakip hangi kanalda ne kadar harcıyor? Doygun kanala girmenin maliyeti hesaplanmış mı?"}
    ]'::jsonb
) ON CONFLICT (slug) DO NOTHING;
