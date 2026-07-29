#!/usr/bin/env python3
"""Build a supervised fine-tuning set for the rubric-analysis task.

Why this is generated rather than collected
-------------------------------------------
Fine-tuning needs examples whose correct answer is known. For this task the
answer is a full rubric filling — nine findings, each with a rating, quotes and
a rationale — and hand-writing enough of those is weeks of work that produces a
few hundred examples of uncertain consistency.

So the generation is inverted. Instead of writing a case and then labelling it,
we *choose the label first*: pick which criteria a case will address and how
well, assemble the case text from fragments that say exactly that, and emit the
findings that must follow. The ground truth is not inferred, it is the input.

What it is teaching
-------------------
Two behaviours, both measured as failures on the base model (see the trial in
mf-backend/scripts/baseline-trial.sh):

1. Raw JSON. The base model wraps every answer in a ```json fence, so strict
   schema adherence measured 0/5 even when the content was complete.

2. Absent evidence. This is the important one. Asked about a criterion the case
   never addresses, the base model writes "the text contains no information
   about competitors" in its rationale and then rates it 3 out of 5 anyway. It
   never emits evidence_found=false with a null score. That single habit means
   coverage always reports 1.0, every report contains fabricated middle ratings,
   and the product's central claim — that a rejection can be defended — is
   false in practice.

   Roughly a third of every generated case is therefore deliberately silent on
   some criteria, and the target output says so.

The prompt is fetched from the running backend rather than reproduced here.
An adapter learns to satisfy one specific instruction; a local copy of the
template would drift the first time either side is edited, and the resulting
adapter would be tuned for a prompt nothing sends — a failure that is invisible,
because training completes normally and the loss looks fine.
"""

from __future__ import annotations

import argparse
import dataclasses
import hashlib
import json
import os
import random
import sys
import urllib.error
import urllib.request

# Share of the *combination space* assigned to the held-out split — not a share
# of rows. Splitting by rows is what let a case appear on both sides; splitting
# the space means a case has one home no matter how many rows are drawn.
#
# Generous at 15% because the eval only needs ~100 rows while training wants
# 800: the binding constraint is having enough distinct cases on the eval side,
# and a stingy share starves it in exactly the small-rubric case that caused
# this. Cases in the eval region that are never drawn cost nothing.
EVAL_SHARE = 0.15

# ---------------------------------------------------------------------------
# Case fragments.
#
# Each entry is one criterion's worth of deck text at a known quality. The
# `score` is the label: it is what the target findings will claim, so a fragment
# and its rating have to agree by construction. `quotes` are spans that appear
# verbatim in the text — they become the evidence, which is what makes the
# citations in the training targets real rather than paraphrased.
#
# Double quotes are deliberately absent from every fragment. The inference path
# neutralises them before the model sees a case (see neutraliseQuotes in
# internal/analysis/schema.go), so training on text containing them would teach
# a distribution the model never meets.
# ---------------------------------------------------------------------------

FRAGMENTS_INVESTMENT: dict[str, list[dict]] = {
    "problem_clarity": [
        {
            "score": 5,
            "text": "PROBLEM\nHedef segmentteki 180.000 işletmenin gider kalemleri içinde bu kalem toplam işletme maliyetinin %38'ini oluşturuyor. 42 saha görüşmesinin 38'inde işletme sahibi kaybı bildiğini ama ispatlayamadığını söyledi.",
            "quotes": ["toplam işletme maliyetinin %38'ini oluşturuyor",
                       "42 saha görüşmesinin 38'inde işletme sahibi kaybı bildiğini ama ispatlayamadığını söyledi"],
            "rationale": 'Problem hem büyüklüğüyle hem sıklığıyla ölçülmüş: gider payı yüzdeyle, yaygınlığı görüşme sayısıyla verilmiş.',
        },
        {
            "score": 3,
            "text": "PROBLEM\nMüşteriler bu gideri önemli bir kalem olarak görüyor ve kayıp şüphesi yaygın. Kaç işletmeyle görüştüğümüzü ve kaçının bunu doğruladığını henüz derli toplu bir yere yazmadık.",
            "quotes": ['Müşteriler bu gideri önemli bir kalem olarak görüyor',
                       'Kaç işletmeyle görüştüğümüzü ve kaçının bunu doğruladığını henüz derli toplu bir yere yazmadık'],
            "rationale": 'Problem doğru yerden tarif edilmiş ama doğrulaması yok; metin kaç görüşmeye dayandığını kendisi de bilmediğini söylüyor.',
        },
        {
            "score": 2,
            "text": "PROBLEM\nİşletmeler dijitalleşmekte zorlanıyor. Bu alanda büyük bir ihtiyaç olduğunu düşünüyoruz ve çözümümüzle bu boşluğu dolduracağız.",
            "quotes": ['İşletmeler dijitalleşmekte zorlanıyor',
                       'Bu alanda büyük bir ihtiyaç olduğunu düşünüyoruz'],
            "rationale": 'Problem bir gözlem değil bir kanaat olarak ifade edilmiş; kimin hangi acısı olduğu ve ne sıklıkta yaşandığı metinde yok.',
        },
    ],
    "market_size": [
        {
            "score": 4,
            "text": "PAZAR\nSektör istatistik kurumunun 2025 kaydı ve meslek birliği dağılımına göre hedef segmentte 2,1 milyon birim var. Birim başına yıllık 1.140 TL abonelikle ulaşılabilir pazar 2,4 milyar TL. İlk üç yıl hedefimiz %4 pay.",
            "quotes": ['ulaşılabilir pazar 2,4 milyar TL',
                       'Sektör istatistik kurumunun 2025 kaydı ve meslek birliği dağılımına göre'],
            "rationale": 'Pazar iki bağımsız kaynağa dayandırılmış ve ulaşılabilir kısım toplam pazardan ayrıştırılarak birim fiyatla hesaplanmış.',
        },
        {
            "score": 3,
            "text": "PAZAR\nHedef segmentte yaklaşık 2 milyon birim olduğunu tahmin ediyoruz. Bu rakam sektör raporlarından derlendi ama tek bir kaynağa dayanmıyor ve ulaşılabilir kısmı henüz ayrıştırmadık.",
            "quotes": ['Hedef segmentte yaklaşık 2 milyon birim olduğunu tahmin ediyoruz',
                       'tek bir kaynağa dayanmıyor ve ulaşılabilir kısmı henüz ayrıştırmadık'],
            "rationale": 'Bir büyüklük verilmiş ama kaynağı belirsiz ve toplam pazar ile ulaşılabilir pazar ayrımı yapılmamış.',
        },
        {
            "score": 1,
            "text": "PAZAR\nGlobal pazar 8 trilyon dolar. Bunun küçük bir kısmını alsak bile çok büyük bir iş çıkar.",
            "quotes": ['Global pazar 8 trilyon dolar',
                       'Bunun küçük bir kısmını alsak bile çok büyük bir iş çıkar'],
            "rationale": 'Tepeden inen bir rakam ve payın nasıl alınacağına dair hiçbir gerekçe yok; metnin kendisi hesabı bir temenniye bağlıyor.',
        },
    ],
    "solution_differentiation": [
        {
            "score": 4,
            "text": "ÇÖZÜM\nRakip ürünler ek donanım kurulumu gerektiriyor; bu birim başına 2.400 TL ve yarım gün montaj demek. Biz sistemin halihazırda ürettiği veriyi okuduğumuz için ek donanım gerekmiyor. Patent başvurusu Mart 2026'da yapıldı.",
            "quotes": ['Biz sistemin halihazırda ürettiği veriyi okuduğumuz için ek donanım gerekmiyor',
                       "Patent başvurusu Mart 2026'da yapıldı"],
            "rationale": 'Fark rakibin maliyetiyle karşılaştırılarak sayısallaştırılmış ve korunabilirliği için atılmış somut bir adım gösterilmiş.',
        },
        {
            "score": 3,
            "text": "ÇÖZÜM\nMevcut veriyi okuduğumuz için ek donanım gerekmiyor. Rakiplerin bir kısmı farklı bir yöntem kullanıyor ama hepsinin nasıl çalıştığını inceleyemedik; korunabilirlik tarafında henüz bir adım atmadık.",
            "quotes": ['Mevcut veriyi okuduğumuz için ek donanım gerekmiyor',
                       'hepsinin nasıl çalıştığını inceleyemedik'],
            "rationale": 'Teknik fark söylenmiş ama rakiplerle karşılaştırması eksik ve metin korunabilirlik için hiçbir adım atılmadığını kendisi belirtiyor.',
        },
        {
            "score": 2,
            "text": "ÇÖZÜM\nDaha kullanıcı dostu bir arayüz sunuyoruz ve yapay zeka destekli öneriler veriyoruz.",
            "quotes": ['Daha kullanıcı dostu bir arayüz sunuyoruz'],
            "rationale": 'İki iddia da rakiplerin sitesine konsa fark edilmeyecek genellikte; ölçülebilir ya da kopyalanması zor bir unsur gösterilmemiş.',
        },
    ],
    "traction": [
        {
            "score": 5,
            "text": "ÇEKİŞ\nOcak 2026'da 3 pilot müşteri ve 47 birim vardı. Temmuz 2026 itibarıyla 28 ödeyen müşteri, 611 birim ve aylık 214.000 TL yinelenen gelir. Son 6 ayda aylık ortalama büyüme %31, aylık müşteri kaybı %2,1.",
            "quotes": ['28 ödeyen müşteri, 611 birim ve aylık 214.000 TL yinelenen gelir',
                       'Son 6 ayda aylık ortalama büyüme %31'],
            "rationale": 'Mutlak büyüklük, büyüme oranı ve müşteri kaybı birlikte ve tarihli verilmiş; iki zaman noktası karşılaştırılabilir durumda.',
        },
        {
            "score": 3,
            "text": "ÇEKİŞ\n11 ödeyen müşterimiz ve 140 birim var. Aylık gelir rakamını paylaşmayı tercih etmiyoruz; büyüme son aylarda istikrarlı ilerliyor.",
            "quotes": ['11 ödeyen müşterimiz ve 140 birim var',
                       'Aylık gelir rakamını paylaşmayı tercih etmiyoruz'],
            "rationale": 'Müşteri sayısı verilmiş ama gelir bilinçli olarak paylaşılmamış ve büyüme bir orana bağlanmadan istikrarlı diye nitelenmiş.',
        },
        {
            "score": 2,
            "text": "ÇEKİŞ\nÜç firmayla pilot görüşmesi yapıldı ve ikisi niyet mektubu imzaladı. Ödeyen müşteriye geçiş için görüşmeler sürüyor.",
            "quotes": ['Üç firmayla pilot görüşmesi yapıldı ve ikisi niyet mektubu imzaladı',
                       'Ödeyen müşteriye geçiş için görüşmeler sürüyor'],
            "rationale": 'Niyet mektubu ve süren görüşme çekiş değil ilgi göstergesi; metinde ödeyen müşteri ya da gelir yok.',
        },
    ],
    "business_model": [
        {
            "score": 4,
            "text": "İŞ MODELİ\nBirim başına aylık abonelik, fiyat 95 TL. Edinme maliyeti 310 TL, ortalama müşteri ömrü 26 ay, brüt marj %71. Bu haliyle edinme maliyeti dördüncü ayda geri dönüyor.",
            "quotes": ['Edinme maliyeti 310 TL, ortalama müşteri ömrü 26 ay, brüt marj %71',
                       'edinme maliyeti dördüncü ayda geri dönüyor'],
            "rationale": 'Gelir modeli, edinme maliyeti, ömür ve marj birlikte verilmiş ve birim ekonomisi geri dönüş süresine kadar hesaplanmış.',
        },
        {
            "score": 3,
            "text": "İŞ MODELİ\nBirim başına aylık abonelik alıyoruz, fiyat 95 TL. Edinme maliyetimizi kabaca biliyoruz ama müşteri yaşam boyu değerini henüz hesaplamadık, o yüzden birim ekonomisi için net bir şey söyleyemiyoruz.",
            "quotes": ['Birim başına aylık abonelik alıyoruz, fiyat 95 TL',
                       'müşteri yaşam boyu değerini henüz hesaplamadık'],
            "rationale": 'Fiyat belli ama birim ekonomisinin diğer yarısı eksik; metin yaşam boyu değerin hesaplanmadığını açıkça söylüyor.',
        },
        {
            "score": 2,
            "text": "İŞ MODELİ\nAbonelik modeliyle çalışıyoruz. Ölçek büyüdükçe maliyetlerin düşeceğini ve kârlılığın geleceğini öngörüyoruz.",
            "quotes": ['Ölçek büyüdükçe maliyetlerin düşeceğini ve kârlılığın geleceğini öngörüyoruz'],
            "rationale": 'Model adlandırılmış ama tek bir sayı yok; kârlılık ölçeğe bağlanmış ve ölçeğin bunu nasıl çözdüğü açıklanmamış.',
        },
    ],
    "team": [
        {
            "score": 5,
            "text": "EKİP\nKurucu ortaklar bu alanda sırasıyla 12 ve 9 yıl çalıştı ve son iki yıldır birlikte. İkisi de tam zamanlı. Teknik ekip 5 kişi, ikisi daha önce aynı ürünü ölçeklendirmiş; satış tarafında segmentin içinden gelen bir yönetici var.",
            "quotes": ['Kurucu ortaklar bu alanda sırasıyla 12 ve 9 yıl çalıştı ve son iki yıldır birlikte',
                       'ikisi daha önce aynı ürünü ölçeklendirmiş'],
            "rationale": 'Alan tecrübesi yıl olarak, birlikte çalışma geçmişi süreyle ve tamamlayıcılık rol bazında gösterilmiş; tam zamanlılık belirtilmiş.',
        },
        {
            "score": 3,
            "text": "EKİP\nKurucu ortaklardan biri bu alanda 8 yıl çalıştı, diğeri yazılım tarafından geliyor. İkisi de şu an tam zamanlı değil; teknik ekip iki kişilik ve daha önce birlikte çalışmadılar.",
            "quotes": ['Kurucu ortaklardan biri bu alanda 8 yıl çalıştı',
                       'İkisi de şu an tam zamanlı değil'],
            "rationale": 'Alan tecrübesi var ama metin tam zamanlılık ve birlikte çalışma geçmişi konusunda iki eksiği kendisi sayıyor.',
        },
        {
            "score": 2,
            "text": "EKİP\nGenç ve dinamik bir ekibiz. Alanında uzman kişilerle çalışıyoruz ve danışmanlarımız var.",
            "quotes": ['Genç ve dinamik bir ekibiz',
                       'Alanında uzman kişilerle çalışıyoruz ve danışmanlarımız var'],
            "rationale": 'Kimin ne yaptığı, ne kadar süredir bu işte olduğu ve tam zamanlı olup olmadığı metinde yok; nitelemeler doğrulanabilir değil.',
        },
    ],
    "competition": [
        {
            "score": 4,
            "text": "REKABET\nÜç yerleşik oyuncu var; ikisi kurumsal segmente, biri kamuya odaklı. Fiyatları bizim hedef müşterimizin bütçesinin 4 katı ve kurulum süreleri haftalarla ölçülüyor. Dolaylı rakip olarak elle tutulan tabloları sayıyoruz — asıl rakibimiz o.",
            "quotes": ['Fiyatları bizim hedef müşterimizin bütçesinin 4 katı ve kurulum süreleri haftalarla ölçülüyor',
                       'Dolaylı rakip olarak elle tutulan tabloları sayıyoruz'],
            "rationale": 'Rakipler segment ve fiyat ekseninde konumlandırılmış ve statüko dolaylı rakip olarak açıkça sayılmış.',
        },
        {
            "score": 3,
            "text": "REKABET\nPazarda iki bilinen oyuncu var, ikisi de kurumsal tarafa odaklı. Fiyatlandırmalarını ve pazar paylarını inceleyemedik; küçük ölçekli müşteride ne kadar aktif olduklarını bilmiyoruz.",
            "quotes": ['Pazarda iki bilinen oyuncu var, ikisi de kurumsal tarafa odaklı',
                       'Fiyatlandırmalarını ve pazar paylarını inceleyemedik'],
            "rationale": 'Rakipler adlandırılmış ama karşılaştırma yapılmamış; metin fiyat ve pay bilgisinin incelenmediğini söylüyor.',
        },
        {
            "score": 1,
            "text": "REKABET\nBu alanda doğrudan bir rakibimiz yok. Benzer işler yapan firmalar var ama bizim yaptığımızı yapan kimse yok.",
            "quotes": ['Bu alanda doğrudan bir rakibimiz yok',
                       'bizim yaptığımızı yapan kimse yok'],
            "rationale": 'Rakipsizlik iddiası pazarın haritalanmadığını gösteriyor; dolaylı rakipler ve statüko hiç değerlendirilmemiş.',
        },
    ],
    "financials_ask": [
        {
            "score": 4,
            "text": "TALEP\n6 milyon TL arıyoruz. Dağılım: %55 ekip, %30 satış, %15 altyapı. Bu tutar 18 ay runway veriyor ve bizi aylık 1,2 milyon TL yinelenen gelire, yani bir sonraki turun eşiğine taşıyor.",
            "quotes": ['Bu tutar 18 ay runway veriyor',
                       'bizi aylık 1,2 milyon TL yinelenen gelire, yani bir sonraki turun eşiğine taşıyor'],
            "rationale": 'Talep kalem bazında dağıtılmış ve hangi kilometre taşına kadar yeteceği ay ve gelir hedefiyle birlikte verilmiş.',
        },
        {
            "score": 3,
            "text": "TALEP\n6 milyon TL yatırım arıyoruz. Bütçenin çoğu ekip büyütmeye ve satışa gidecek. Bu tutarın bizi hangi kilometre taşına kadar taşıyacağını ay bazında henüz çıkarmadık.",
            "quotes": ['6 milyon TL yatırım arıyoruz',
                       'Bu tutarın bizi hangi kilometre taşına kadar taşıyacağını ay bazında henüz çıkarmadık'],
            "rationale": 'Tutar ve kaba dağılım var ama runway hesabı yok; metin kilometre taşı bağlantısının kurulmadığını kendisi belirtiyor.',
        },
        {
            "score": 2,
            "text": "TALEP\nBüyümek için yatırıma ihtiyacımız var. Gelen kaynağı büyüme ve ekip için kullanacağız.",
            "quotes": ['Büyümek için yatırıma ihtiyacımız var',
                       'Gelen kaynağı büyüme ve ekip için kullanacağız'],
            "rationale": 'Tutar belirtilmemiş, kullanım planı iki kelimeyle geçilmiş ve hangi hedefe kadar yeteceğine dair bir hesap yok.',
        },
    ],
    "risk": [
        {
            "score": 4,
            "text": "RİSKLER\nEn büyük risk, veri sağlayıcıların erişimi kısıtlaması; bu gerçekleşirse ürünün girdi kaynağı kesilir. İki sağlayıcıyla sözleşme görüşmesi sürüyor ve bağımsız bir alternatif okuma prototipi çalışıyor.",
            "quotes": ['En büyük risk, veri sağlayıcıların erişimi kısıtlaması',
                       'bağımsız bir alternatif okuma prototipi çalışıyor'],
            "rationale": 'Ana risk sonucuyla birlikte adlandırılmış ve azaltma için atılmış iki somut adım gösterilmiş.',
        },
        {
            "score": 3,
            "text": "RİSKLER\nEn belirgin risk veri erişiminin kısıtlanması. Bunun ne kadar yakın bir tehdit olduğunu ölçemedik ve alternatif bir yol üzerinde henüz çalışmaya başlamadık.",
            "quotes": ['En belirgin risk veri erişiminin kısıtlanması',
                       'alternatif bir yol üzerinde henüz çalışmaya başlamadık'],
            "rationale": 'Risk doğru teşhis edilmiş ama ne olasılığı ne de azaltma planı var; metin hazırlığın başlamadığını söylüyor.',
        },
        {
            "score": 1,
            "text": "RİSKLER\nÖnümüzde ciddi bir engel görmüyoruz, plan net.",
            "quotes": ['Önümüzde ciddi bir engel görmüyoruz'],
            "rationale": 'Hiçbir risk tanımlanmamış; bu riski olmayan bir işi değil, riski değerlendirmemiş bir ekibi gösterir.',
        },
    ],
}

# The digital-marketing rubric's bank. Same construction, same rules: the score
# is the label and the text has to earn it, the quotes appear verbatim, and no
# double quote survives anywhere (the inference path neutralises them).
#
# The strong/weak split is chosen to mirror how marketing briefs actually fail.
# A weak fragment here is rarely empty — it is confident and unmeasurable, which
# is the harder case: a model that scores on tone rather than evidence rates
# 'markamızı herkesin tanımasını istiyoruz' as a stated goal instead of as an
# absent one. That is the same failure the absent branch exists to teach, met
# from the other direction.
FRAGMENTS_MARKETING: dict[str, list[dict]] = {
    "audience_clarity": [
        {
            "score": 5,
            "text": "HEDEF KİTLE\nBirincil kitle: üç büyük ilde 28-40 yaş arası, ayda en az iki kez satın alan çalışan ebeveynler. Panel verimize göre bu grubun %64'ü akşam 20.00-23.00 arasında mobilden alıyor ve sepet ortalaması 780 TL. Öğrencileri bilinçli olarak dışarıda bıraktık: sepet ortalaması 210 TL, kampanyasız dönüş %3.",
            "quotes": ["bu grubun %64'ü akşam 20.00-23.00 arasında mobilden alıyor",
                       'Öğrencileri bilinçli olarak dışarıda bıraktık'],
            "rationale": 'Kitle davranış ve niyetle tanımlanmış, bulunduğu mecra ve saat verilmiş, ve dışarıda bırakılan segment gerekçesiyle belirtilmiş.',
        },
        {
            "score": 4,
            "text": "HEDEF KİTLE\nKitleyi satın alma davranışına göre üç gruba ayırdık: düzenli alanlar (%22, ciro payı %61), ayda bir-iki kez alanlar (%45) ve kampanya dönemlerinde gelenler (%33). Kampanyayı ilk iki gruba kuruyoruz. Üçüncü grubun geri dönüş oranı iki çeyrektir %5'in altında.",
            "quotes": ['Kitleyi satın alma davranışına göre üç gruba ayırdık',
                       "Üçüncü grubun geri dönüş oranı iki çeyrektir %5'in altında"],
            "rationale": 'Segmentasyon davranışa ve ciro payına dayanıyor ve hangi grubun neden hedeflenmediği veriyle gerekçelendirilmiş.',
        },
        {
            "score": 3,
            "text": "HEDEF KİTLE\nBirincil kitle büyükşehirlerde yaşayan, dijital kanalları aktif kullanan 30-45 yaş arası çalışanlar. Satın alma saatleri ve ortalama harcama hakkında elimizde panel verisi yok, kendi kayıtlarımızdan genel bir izlenimimiz var.",
            "quotes": ['Birincil kitle büyükşehirlerde yaşayan, dijital kanalları aktif kullanan 30-45 yaş arası çalışanlar',
                       'elimizde panel verisi yok'],
            "rationale": 'Kitle demografiyle daraltılmış ama davranış tarafı ölçülmemiş; metin veri eksikliğini kendisi kabul ediyor.',
        },
        {
            "score": 2,
            "text": "HEDEF KİTLE\nHedef kitlemiz 18-55 yaş arası, interneti aktif kullanan, kaliteye önem veren tüketiciler. Ülke genelinde herkese ulaşmak istiyoruz.",
            "quotes": ['Hedef kitlemiz 18-55 yaş arası, interneti aktif kullanan, kaliteye önem veren tüketiciler',
                       'Ülke genelinde herkese ulaşmak istiyoruz'],
            "rationale": 'Tanım neredeyse tüm nüfusu kapsıyor; daraltıcı hiçbir davranış ya da niyet ölçütü yok.',
        },
    ],
    "channel_fit": [
        {
            "score": 4,
            "text": "KANALLAR\nBütçenin %55'i sosyal, %30'u arama ağı, %15'i kısa video. Sosyal ağırlığının sebebi kitlenin akşam mobil kullanımı; arama ağı niyet hazır olduğu için yeniden satın almada kullanılacak. Kısa video test bütçesi: üç ay sonunda edinme maliyeti 220 TL'nin altına inmezse kapatılacak.",
            "quotes": ['Sosyal ağırlığının sebebi kitlenin akşam mobil kullanımı',
                       "üç ay sonunda edinme maliyeti 220 TL'nin altına inmezse kapatılacak"],
            "rationale": 'Her kanal kitlenin davranışıyla gerekçelendirilmiş ve test kanalı için önceden bir kapatma eşiği tanımlanmış.',
        },
        {
            "score": 3,
            "text": "KANALLAR\nBütçenin çoğunu sosyal ve arama ağına ayıracağız, çünkü geçmiş kampanyalarımız oradan dönüş getirdi. Dağılımı henüz yüzdeyle sabitlemedik ve hangi kanalın hangi aşamaya hizmet ettiğini ayrıştırmadık.",
            "quotes": ['Bütçenin çoğunu sosyal ve arama ağına ayıracağız',
                       'Dağılımı henüz yüzdeyle sabitlemedik'],
            "rationale": 'Kanal seçimi geçmiş performansa dayanıyor ama dağılım ve her kanalın hangi aşamaya hizmet ettiği belirlenmemiş.',
        },
        {
            "score": 2,
            "text": "KANALLAR\nAğırlığı mesleki ağa vereceğiz çünkü orada daha profesyonel bir kitle var ve marka algımıza uygun. Ürünümüz günlük tüketim ürünü ama o mecranın prestiji bize daha çok yakışıyor.",
            "quotes": ['Ağırlığı mesleki ağa vereceğiz çünkü orada daha profesyonel bir kitle var',
                       'Ürünümüz günlük tüketim ürünü ama o mecranın prestiji bize daha çok yakışıyor'],
            "rationale": 'Kanal, kitlenin bulunduğu yere göre değil marka algısına göre seçilmiş ve metin ürünle mecra arasındaki uyumsuzluğu kendisi söylüyor.',
        },
        {
            "score": 1,
            "text": "KANALLAR\nTüm büyük mecralarda aynı anda varlık göstereceğiz. Hangi kanalın işe yaradığını ilerleyen aylarda göreceğiz.",
            "quotes": ['Tüm büyük mecralarda aynı anda varlık göstereceğiz',
                       'Hangi kanalın işe yaradığını ilerleyen aylarda göreceğiz'],
            "rationale": 'Kanal seçimi yapılmamış; bütçe her yere dağıtılıp karar sonraya bırakılmış, bu da hiçbir kanalda anlamlı hacim olmaması demek.',
        },
    ],
    "budget_realism": [
        {
            "score": 5,
            "text": "BÜTÇE\nAylık 450.000 TL, altı ay. Hedef 9.000 yeni müşteri, yani müşteri başına 300 TL üst sınır. Son üç çeyrekte gerçekleşen edinme maliyeti sırasıyla 268, 291 ve 275 TL; en kötü çeyrek tekrarlansa hedef 8.900'de kalıyor ve bu sapmayı kabul ediyoruz. Yaratıcı üretim bütçesi bunun dışında, 60.000 TL.",
            "quotes": ['Son üç çeyrekte gerçekleşen edinme maliyeti sırasıyla 268, 291 ve 275 TL',
                       "en kötü çeyrek tekrarlansa hedef 8.900'de kalıyor ve bu sapmayı kabul ediyoruz"],
            "rationale": 'Hedef × gerçekleşen edinme maliyeti bütçeyle çarpıştırılmış ve en kötü senaryo ayrıca hesaplanıp kabul edilmiş.',
        },
        {
            "score": 4,
            "text": "BÜTÇE\nÜç aylık medya bütçesi 1.200.000 TL. Hedef 4.000 yeni ödeyen müşteri. Geçen çeyrekte gerçekleşen edinme maliyetimiz 265 TL; aynı maliyetle hedef 1.060.000 TL tutuyor ve kalan pay yaratıcı üretimi ile teste kalıyor. Edinme maliyeti 300 TL'yi aşarsa hedef 3.500'e çekilecek.",
            "quotes": ['Geçen çeyrekte gerçekleşen edinme maliyetimiz 265 TL',
                       "Edinme maliyeti 300 TL'yi aşarsa hedef 3.500'e çekilecek"],
            "rationale": 'Bütçe gerçekleşmiş edinme maliyetiyle hedefe bağlanmış ve maliyet artarsa hedefin nasıl güncelleneceği önceden yazılmış.',
        },
        {
            "score": 3,
            "text": "BÜTÇE\nÜç aylık bütçe 600.000 TL ve hedefimiz 2.500 yeni müşteri. Geçmiş edinme maliyetimiz kampanyadan kampanyaya 200 ile 400 TL arasında değişti; hangi senaryoda hedefi tutturacağımızı ayrıca hesaplamadık.",
            "quotes": ['Üç aylık bütçe 600.000 TL ve hedefimiz 2.500 yeni müşteri',
                       'hangi senaryoda hedefi tutturacağımızı ayrıca hesaplamadık'],
            "rationale": 'Bütçe ve hedef verilmiş ama edinme maliyeti aralığı geniş ve metin senaryo hesabının yapılmadığını belirtiyor.',
        },
        {
            "score": 2,
            "text": "BÜTÇE\nAylık 40.000 TL reklam bütçemizle ilk yıl 50.000 yeni müşteriye ulaşmayı hedefliyoruz. Dijitalin maliyet avantajıyla bu rakam fazlasıyla mümkün.",
            "quotes": ['Aylık 40.000 TL reklam bütçemizle ilk yıl 50.000 yeni müşteriye ulaşmayı hedefliyoruz',
                       'Dijitalin maliyet avantajıyla bu rakam fazlasıyla mümkün'],
            "rationale": "Bütçe hedefe bölündüğünde müşteri başına 10 TL'nin altına düşüyor; metin bu farkı bir maliyet avantajı ifadesiyle geçiştiriyor.",
        },
    ],
    "differentiation": [
        {
            "score": 4,
            "text": "MESAJ\nAna vaat: ilk kurulum 24 saatte tamamlanır, gecikirse o ayın ücreti alınmaz. Rakiplerin hiçbiri gecikmeye bağlı bir taahhüt vermiyor; ikisi hızdan söz ediyor ama süre belirtmiyor. Mesajı test ettiğimiz 600 kişilik grupta hatırlanma oranı jenerik hız mesajına göre 1,8 kat çıktı.",
            "quotes": ['gecikirse o ayın ücreti alınmaz',
                       'Rakiplerin hiçbiri gecikmeye bağlı bir taahhüt vermiyor'],
            "rationale": 'Vaat ölçülebilir ve taahhüde bağlanmış, rakiplerin mesajıyla karşılaştırılmış ve hatırlanma testiyle doğrulanmış.',
        },
        {
            "score": 3,
            "text": "MESAJ\nAna vaadimiz hızlı hizmet ve geniş kapsam. Rakiplerin de hızdan söz ettiğini biliyoruz; mesajımızı onlarınkiyle yan yana koyup test etmedik.",
            "quotes": ['Ana vaadimiz hızlı hizmet ve geniş kapsam',
                       'mesajımızı onlarınkiyle yan yana koyup test etmedik'],
            "rationale": 'Vaat tanımlı ama rakiplerinkinden ayrışmıyor ve metin karşılaştırmalı testin yapılmadığını söylüyor.',
        },
        {
            "score": 2,
            "text": "MESAJ\nKaliteli ürünleri uygun fiyata sunan, müşteri memnuniyetini ön planda tutan bir marka olduğumuzu anlatacağız.",
            "quotes": ['Kaliteli ürünleri uygun fiyata sunan, müşteri memnuniyetini ön planda tutan bir marka'],
            "rationale": 'Mesaj rakibin sitesine konsa fark edilmez; hiçbir unsuru markaya özgü ya da ölçülebilir değil.',
        },
        {
            "score": 1,
            "text": "MESAJ\nSektörün lideri olarak müşterilerimize en iyi deneyimi sunuyoruz. Yenilikçi ve güvenilir yaklaşımımızla fark yaratıyoruz.",
            "quotes": ['Sektörün lideri olarak müşterilerimize en iyi deneyimi sunuyoruz',
                       'Yenilikçi ve güvenilir yaklaşımımızla fark yaratıyoruz'],
            "rationale": 'Liderlik iddiası dayanaksız ve konumlandırma tamamen sıfatlardan oluşuyor; ayrıştırıcı tek bir somut unsur yok.',
        },
    ],
    "measurement_plan": [
        {
            "score": 5,
            "text": "ÖLÇÜM\nKuzey yıldızı metrik: ilk 30 günde ikinci alışverişini yapan yeni müşteri sayısı. Atıf için son tıklama yerine 7 günlük veri odaklı model kullanılacak, iki bağımsız kaynak çapraz kontrol edilecek. Beğeni ve erişim raporlanacak ama karar metriği sayılmayacak. Haftalık okuma salı sabahı, kanal kapatma kararı en erken dördüncü haftada.",
            "quotes": ['Kuzey yıldızı metrik: ilk 30 günde ikinci alışverişini yapan yeni müşteri sayısı',
                       'Beğeni ve erişim raporlanacak ama karar metriği sayılmayacak'],
            "rationale": 'Tek bir karar metriği seçilmiş, atıf modeli ve kaynağı belirtilmiş, ve gösterge metrikler karar dışında bırakılmış.',
        },
        {
            "score": 4,
            "text": "ÖLÇÜM\nBaşarı ölçütü altı ay sonundaki tekrar satın alma oranı; eşik %35, bugün %29. Ölçüm iki sistemin haftalık eşleştirilmesiyle yapılacak. Atıf modelini kampanya başında sabitliyoruz ki ortada değiştirip sonucu kendimize göre okumayalım.",
            "quotes": ['Başarı ölçütü altı ay sonundaki tekrar satın alma oranı; eşik %35, bugün %29',
                       'Atıf modelini kampanya başında sabitliyoruz'],
            "rationale": 'Eşik ve mevcut değer birlikte verilmiş, ve atıf modelinin önceden sabitlenmesi sonucun sonradan yorumlanmasını engelliyor.',
        },
        {
            "score": 3,
            "text": "ÖLÇÜM\nDönüşüm sayısını ve edinme maliyetini haftalık takip edeceğiz. Atıf modelini varsayılanda bırakıyoruz ve hangi metriğin karar metriği olduğunu ekipte netleştirmedik.",
            "quotes": ['Dönüşüm sayısını ve edinme maliyetini haftalık takip edeceğiz',
                       'hangi metriğin karar metriği olduğunu ekipte netleştirmedik'],
            "rationale": 'Takip edilecek metrikler belli ama hangisinin karar vereceği belirsiz ve atıf modeli bilinçli bir seçim değil.',
        },
        {
            "score": 2,
            "text": "ÖLÇÜM\nKampanya sonunda erişim, gösterim ve etkileşim sayılarını raporlayacağız. Beğeni ve yorum artışı başarının en net göstergesi olacak.",
            "quotes": ['Beğeni ve yorum artışı başarının en net göstergesi olacak'],
            "rationale": 'Ölçüm tamamen gösterge metriklere dayanıyor; satın almaya ya da gelire bağlanan tek bir metrik yok.',
        },
    ],
    "competitive_context": [
        {
            "score": 4,
            "text": "REKABET\nPazar analizi araçları ve reklam kütüphanesi taramamıza göre iki büyük rakip arama ağında marka kelimelerimize de teklif veriyor ve tıklama maliyetini son altı ayda %40 yukarı çekti. Bu yüzden arama ağında yalnızca yüksek niyetli uzun kuyruk kelimelerde yarışacağız, marka savunması dışında genele girmeyeceğiz.",
            "quotes": ['iki büyük rakip arama ağında marka kelimelerimize de teklif veriyor',
                       'arama ağında yalnızca yüksek niyetli uzun kuyruk kelimelerde yarışacağız'],
            "rationale": 'Rakiplerin kanal davranışı araçla ölçülmüş, maliyete etkisi sayısallaştırılmış ve buna göre bir kaçınma stratejisi kurulmuş.',
        },
        {
            "score": 3,
            "text": "REKABET\nİki büyük rakibin reklam verdiğini görüyoruz ve arama ağında maliyetlerin yükseldiğini fark ettik. Ne kadar harcadıklarına ve hangi kelimelerde yoğunlaştıklarına dair bir çalışma yapmadık.",
            "quotes": ['İki büyük rakibin reklam verdiğini görüyoruz',
                       'Ne kadar harcadıklarına ve hangi kelimelerde yoğunlaştıklarına dair bir çalışma yapmadık'],
            "rationale": 'Rakip varlığı fark edilmiş ve maliyet etkisi sezilmiş ama harcama ve kelime düzeyinde bir inceleme yapılmamış.',
        },
        {
            "score": 2,
            "text": "REKABET\nRakiplerimizin de dijitalde aktif olduğunu biliyoruz ve benzer kanalları kullanıyorlar. Biz daha yaratıcı içeriklerle öne çıkacağız.",
            "quotes": ['Rakiplerimizin de dijitalde aktif olduğunu biliyoruz ve benzer kanalları kullanıyorlar',
                       'Biz daha yaratıcı içeriklerle öne çıkacağız'],
            "rationale": 'Rakip davranışı genel bir cümleyle geçilmiş; doygun kanala girmenin maliyeti hesaplanmamış ve fark yaratıcılığa havale edilmiş.',
        },
        {
            "score": 1,
            "text": "REKABET\nBu alanda ciddi bir rakip görmüyoruz, pazar bizim için açık.",
            "quotes": ['Bu alanda ciddi bir rakip görmüyoruz, pazar bizim için açık'],
            "rationale": 'Rakiplerin kanal davranışı hiç değerlendirilmemiş; pazarın açık olduğu iddiası hiçbir gözleme dayanmıyor.',
        },
    ],
}

# What changes between rubrics, and nothing else. The case assembly, the
# rationale banks and the RNG call sequence are shared, which is what keeps the
# investment set byte-identical to the one already trained on: adding this table
# introduced no new rng call on that path.
DOMAIN_BANKS: dict[str, dict] = {
    "startup-investability": {
        "fragments": FRAGMENTS_INVESTMENT,
        "title_suffix": "yatırım sunumu",
        # 4 of 9. The value the old max(4, len(keys) - 5) produced here, kept
        # literal so this rubric's set stays byte-identical to the one already
        # generated — rng.randint consumes entropy as a function of its range,
        # so even an arithmetically equal bound written differently is safe only
        # because it computes to the same two numbers.
        "min_present": 4,
    },
    "digital-marketing": {
        "fragments": FRAGMENTS_MARKETING,
        "title_suffix": "pazarlama brief'i",
        # 3 of 6, chosen to match investment's share of absent findings rather
        # than its floor. See build_case.
        "min_present": 3,
    },
}

COMPANY_NAMES = [
    "Veriform", "Akıştan", "Nirengi", "Bakiye", "Menzil",
    "Tutamak", "Basamak", "Denge Labs", "Ölçek", "Kerteriz",
]

# Rationales for findings that HAVE evidence live on the fragment itself, so
# they can name what earned the score. There is no bank for them any more: one
# existed, it held seven sentences, and every rating in the training set was
# justified by one of those seven regardless of what the case said.
#
# The absent branch keeps a bank, and can, because there is nothing case-specific
# to say — the case is silent, and the finding's whole content is that it is.
# Varied so the model learns the behaviour rather than one sentence.
RATIONALES_ABSENT = [
    "Metinde bu kritere dair bilgi bulunmuyor.",
    "Vaka metni bu konuya hiç değinmiyor, değerlendirilemedi.",
    "Bu kriteri değerlendirecek bir ifade metinde yok.",
    "Metin bu başlıkta sessiz; puanlanacak dayanak yok.",
]


def fetch_prompt(base_url: str, token: str, domain: str) -> dict:
    """Read the exact instruction the backend generates for this rubric."""
    req = urllib.request.Request(
        f"{base_url}/analysis/domains/{domain}/prompt",
        headers={"Authorization": f"Bearer {token}"},
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        sys.exit(f"could not fetch the prompt ({e.code}); is the backend running "
                 f"and the token valid?\n{e.read().decode(errors='replace')[:400]}")
    except urllib.error.URLError as e:
        sys.exit(f"could not reach {base_url}: {e.reason}")


def neutralise_quotes(s: str) -> str:
    """Mirror of neutraliseQuotes in internal/analysis/schema.go.

    Duplicated rather than fetched because it applies to text this script
    generates, not to text the backend has seen. Kept deliberately trivial so
    the duplication cannot rot: if either side grows a rule, the training
    distribution stops matching inference, which is the failure this whole file
    is arranged to avoid.
    """
    for q in ('"', "“", "”", "„", "«", "»"):
        s = s.replace(q, "'")
    return s


@dataclasses.dataclass
class CaseSpec:
    """Everything drawn at random for one case, separated from its rendering.

    Split out so a case can be rebuilt exactly, or rebuilt with one deliberate
    change and nothing else. That is what build_contrast_set.py needs: a pair
    that differs in a single criterion, with the same company, the same section
    order and the same fragments everywhere else — otherwise a difference in the
    model's answer could be explained by any of them.
    """

    picked: dict[str, int]              # present criterion -> fragment index
    company: str
    order: list[str]                    # section order, present criteria only
    quotes_n: dict[str, int]            # present criterion -> how many quotes cited
    absent_rationale: dict[str, str]    # absent criterion -> which sentence


def draw_spec(rng: random.Random, criteria: list[dict], bank: dict) -> CaseSpec:
    """Draw one case's random choices.

    The order of the rng calls here is load-bearing: it reproduces the sequence
    build_case used before the spec was split out, which is what keeps already
    generated sets byte-identical. Anything added must go at the end.
    """
    fragments = bank["fragments"]
    keys = [c["key"] for c in criteria]

    n_present = rng.randint(bank["min_present"], len(keys))
    present = set(rng.sample(keys, n_present))
    company = rng.choice(COMPANY_NAMES)

    picked: dict[str, int] = {}
    for key in keys:
        if key in present:
            picked[key] = rng.randrange(len(fragments[key]))

    order = list(picked.keys())
    rng.shuffle(order)

    quotes_n: dict[str, int] = {}
    absent_rationale: dict[str, str] = {}
    for c in criteria:
        key = c["key"]
        if key in present:
            frag = fragments[key][picked[key]]
            quotes_n[key] = rng.randint(1, min(2, len(frag["quotes"])))
        else:
            absent_rationale[key] = rng.choice(RATIONALES_ABSENT)

    return CaseSpec(picked=picked, company=company, order=order,
                    quotes_n=quotes_n, absent_rationale=absent_rationale)


def render_case(criteria: list[dict], bank: dict,
                spec: CaseSpec) -> tuple[str, str, list[dict], tuple]:
    """Turn a spec into (title, case text, findings, signature). No randomness."""
    fragments = bank["fragments"]
    title = f"{spec.company} — {bank['title_suffix']}"

    sections = [fragments[key][spec.picked[key]]["text"] for key in spec.order]

    findings: list[dict] = []
    for c in criteria:
        key = c["key"]
        if key not in spec.picked:
            findings.append({
                "key": key,
                "evidence_found": False,
                "score": None,
                "evidence": [],
                "rationale": spec.absent_rationale[key],
            })
            continue

        frag = fragments[key][spec.picked[key]]
        quotes = frag["quotes"][: spec.quotes_n[key]]
        findings.append({
            "key": key,
            "evidence_found": True,
            "score": frag["score"],
            "evidence": [neutralise_quotes(q) for q in quotes],
            # The fragment's own rationale, not one drawn from a bank of generic
            # sentences. Those produced eleven distinct strings across twelve
            # thousand findings, so the adapter learned to justify every rating
            # with the same handful of phrases — and a report whose reasoning is
            # interchangeable is not a report anyone can argue with. Written
            # alongside the text it describes, it can name the thing that earned
            # the score, which is the whole difference between a rating and a
            # finding.
            "rationale": frag["rationale"],
        })

    body = f"{spec.company}\n\n" + "\n\n".join(sections)
    signature = tuple(sorted(spec.picked.items()))
    return title, neutralise_quotes(body), findings, signature


def build_case(rng: random.Random, criteria: list[dict],
               bank: dict) -> tuple[str, str, list[dict], tuple]:
    """Assemble one case and the findings that must follow from it.

    `bank` is one entry of DOMAIN_BANKS: the fragment set keyed by criterion and
    the words the case titles itself with. Everything else here is shared across
    rubrics, deliberately — the behaviour being taught (cite what is there, say
    so when it is not) is the same behaviour whichever rubric asks for it.

    Returns (title, case text, findings, signature).

    The signature is what identifies this case as *content*: which criteria it
    addresses and which fragment each of them drew. Two cases with the same
    signature read differently — the company name and the section order are
    drawn separately — but they carry identical information, so a model cannot
    tell them apart in any way that matters. That is why the split is assigned
    on this and not on the rendered text.
    """
    fragments = bank["fragments"]
    keys = [c["key"] for c in criteria]

    # A rubric whose criteria the bank does not cover would otherwise fail deep
    # inside the loop below with a bare KeyError, after the generator has
    # already reported how many examples it is about to write.
    missing = [k for k in keys if k not in fragments]
    if missing:
        sys.exit(f"no fragments for criteria: {', '.join(missing)}\n"
                 f"the rubric and the fragment bank have drifted apart — add "
                 f"them to DOMAIN_BANKS before generating")

    # How many criteria a case addresses, which fragment each draws, the company
    # and the section order all come from draw_spec; rendering them is separate
    # and does no drawing. See CaseSpec for why the two halves are split.
    #
    # A note that lives here because this is where it used to be decided: the
    # floor on how many criteria are present is per-rubric rather than derived
    # from the criterion count. It used to be max(4, len(keys) - 5), which is 4
    # for the 9-criterion investment rubric and also 4 for the 6-criterion
    # marketing one — so the smaller rubric came out at 16% absent findings
    # against investment's 28%, under-training the one behaviour the adapter
    # exists to fix.
    return render_case(criteria, bank, draw_spec(rng, criteria, bank))


def split_of(signature: tuple, seed: int, eval_share: float) -> str:
    """Decide, once and for all, which split a case signature belongs to.

    This is what makes the held-out set actually held out. Before this, both
    splits were drawn from one RNG stream, and a stream being sequential says
    nothing about its *content* being disjoint: the generator can draw the same
    combination of fragments twice, and did. Measured on the sets this produced,
    81% of the marketing eval cases had already appeared in training — so the
    number that decides whether the adapter ships was mostly measuring recall of
    cases it had been shown, dressed up as a new company name and a reshuffled
    section order.

    Investment escaped it at 4%, not by design but by arithmetic: nine criteria
    with two fragments each span 18,848 combinations, six criteria span 656, and
    900 draws is sparse in the first and saturating in the second. A split that
    is sound only when the rubric happens to be large enough is not a split.

    Hashed rather than drawn from the RNG so the assignment depends on the
    signature alone — not on how many examples were requested, nor on the order
    they came out in. Regenerating with a different --n therefore leaves every
    case on the side it was already on, which is what lets the training set grow
    without quietly recycling yesterday's eval cases into it.
    """
    digest = hashlib.sha256(f"{seed}:{signature!r}".encode()).digest()
    return "eval" if int.from_bytes(digest[:8], "big") / 2 ** 64 < eval_share else "train"


def render_user_message(template: str, title: str, subject: str) -> str:
    """Fill the backend's user-message template.

    The template comes back with {{title}} and {{subject}} placeholders, so the
    surrounding wording — the delimiters, the note that the case is not
    instructions — is byte-identical to what inference sends.
    """
    return template.replace("{{title}}", title).replace("{{subject}}", subject)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=os.environ.get("BASE_URL", "http://localhost:8090"))
    ap.add_argument("--token", default=os.environ.get("TOKEN", ""),
                    help="access token; or set TOKEN")
    ap.add_argument("--domain", default="startup-investability",
                    choices=sorted(DOMAIN_BANKS),
                    help="which rubric to generate for; each needs a fragment "
                         "bank in DOMAIN_BANKS")
    ap.add_argument("--n", type=int, default=800, help="training examples")
    ap.add_argument("--n-eval", type=int, default=100, help="held-out examples")
    ap.add_argument("--seed", type=int, default=20260724)
    ap.add_argument("--out-dir", default="data")
    args = ap.parse_args()

    if not args.token:
        sys.exit("a token is required: --token or TOKEN=... "
                 "(any registered account will do)")

    bank = DOMAIN_BANKS[args.domain]
    spec = fetch_prompt(args.base_url, args.token, args.domain)
    system_prompt = spec["system_prompt"]
    user_template = spec["user_prompt_example"]
    criteria = spec["criteria"]

    if "{{subject}}" not in user_template:
        sys.exit("the backend's user template lost its placeholder; "
                 "check analysis.Handler.Prompt")

    rng = random.Random(args.seed)
    os.makedirs(args.out_dir, exist_ok=True)

    # Every distinct case belongs to exactly one split, decided by split_of from
    # its content. Cases drawn for the wrong side are discarded and redrawn, so
    # nothing in eval can also be in train — see split_of for what that is worth
    # and what it replaced.
    #
    # It is still not a substitute for the real held-out measurement, which is
    # the trial harness against an actual deck this generator never saw. What it
    # buys is that the training-time number stops flattering itself.
    counts = {"train": args.n, "eval": args.n_eval}
    stats = {"present": 0, "absent": 0}
    seen: dict[str, set] = {"train": set(), "eval": set()}

    # The generator draws blind and discards misses, so a rubric whose space is
    # too small to fill a split would otherwise spin here forever. The ceiling
    # is generous — misses are cheap — but finite, and the message says which of
    # the two numbers to change.
    max_attempts_per_row = 200

    for split, n in counts.items():
        path = os.path.join(args.out_dir, f"{split}.jsonl")
        with open(path, "w", encoding="utf-8") as fh:
            attempts = 0
            written = 0
            while written < n:
                attempts += 1
                if attempts > n * max_attempts_per_row:
                    sys.exit(
                        f"could only place {written} of {n} {split} cases before "
                        f"giving up.\nThe rubric's combination space is too small "
                        f"for a split this size — add fragments to DOMAIN_BANKS"
                        f"['{args.domain}'], or ask for fewer examples.")

                title, subject, findings, signature = build_case(rng, criteria, bank)
                if split_of(signature, args.seed, EVAL_SHARE) != split:
                    continue
                seen[split].add(signature)
                written += 1

                for f in findings:
                    stats["present" if f["evidence_found"] else "absent"] += 1

                # separators without spaces, and the assistant message is bare
                # JSON with no fence — that absence is half of what is being
                # taught, so it must be exact.
                target = json.dumps({"findings": findings},
                                    ensure_ascii=False, separators=(",", ":"))

                fh.write(json.dumps({
                    "messages": [
                        {"role": "system", "content": system_prompt},
                        {"role": "user", "content": render_user_message(user_template, title, subject)},
                        {"role": "assistant", "content": target},
                    ]
                }, ensure_ascii=False) + "\n")
        print(f"  {path}: {n} examples, {len(seen[split])} distinct cases")

    total = stats["present"] + stats["absent"]
    share = stats["absent"] / total if total else 0
    print(f"\nrubric: {spec['domain']} v{spec['version']}, {len(criteria)} criteria")
    print(f"findings: {total} total, {stats['absent']} absent ({share:.0%})")

    # Proof rather than promise. The split is disjoint by construction, so this
    # can only fail if split_of stops being a function of the signature — and if
    # that ever happens, saying so here costs nothing and finding out from a
    # suspiciously good eval costs a training run.
    leak = seen["train"] & seen["eval"]
    assert not leak, f"{len(leak)} case(s) landed in both splits"

    # Distinct cases against rows written: the ratio the sets were failing on.
    # 800 rows drawn from 457 distinct marketing cases is not 800 examples worth
    # of information, and nothing downstream reports that.
    for split in ("train", "eval"):
        rows = counts[split]
        distinct = len(seen[split])
        if distinct < rows * 0.9:
            print(f"WARNING: {split} has {distinct} distinct cases across {rows} "
                  f"rows — the fragment bank is too small for this many examples")

    if share < 0.15:
        print("WARNING: too few absent findings to teach the behaviour that "
              "matters; widen the sampling in build_case")


if __name__ == "__main__":
    main()
