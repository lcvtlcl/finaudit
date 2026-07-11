#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Генератор синтетических тестовых банковских выписок для FinAudit.

Форматы:
  1. CSV банковской выписки — два подвида:
       a) "bank" — именованные колонки (совместим с parseBankStatementRows):
          Дата операции; Номер пп; Тип операции; Наименование контрагента;
          ИНН контрагента; КПП контрагента; Расчетный счет контрагента;
          БИК банка контрагента; Банк контрагента; Назначение платежа;
          Сумма дебет (руб.); Сумма кредит (руб.); Валюта;
          Остаток на конец дня (руб.); Категория
          Разделитель — точка с запятой (парсер жёстко ждёт ';').
       b) "legacy" — позиционный, 6 колонок:
          дата; сумма(знаковая); направление; контрагент; ИНН; назначение
          Разделители: ';' (парсится) и ',' (вариант для тестов; парсер csv
          в проекте использует ';', поэтому запятая — это негативный/robustness кейс).
  2. 1CClientBankExchange (.txt, Windows-1251) — строго по формату из
     internal/ingest/clientbank.go.
  3. XLSX (openpyxl) — те же именованные колонки, что и bank-CSV.

Детерминированно: seed зафиксирован.
"""

import csv
import io
import os
import random
from datetime import date, timedelta

SEED = 20260702
random.seed(SEED)

OUT = os.path.dirname(os.path.abspath(__file__))

# ---------------------------------------------------------------------------
# Справочники контрагентов и реквизитов
# ---------------------------------------------------------------------------

# Наш банк / реквизиты владельца счёта задаются в каждом профиле.

TAX_INN = "7727406020"   # УФК (условно, для налогов)
TAX_NAME = "УФК по г. Москве (ИФНС России)"
TAX_KPP = "770801001"
TAX_ACC = "40101810045250010041"
TAX_BIK = "004525988"
TAX_BANK = "ГУ Банка России по ЦФО//УФК по г. Москве"

SFR_INN = "7703363868"
SFR_NAME = "СФР по г. Москве и МО"

BANKS = {
    "sber":  ("044525225", "ПАО СБЕРБАНК"),
    "tbank": ("044525974", "АО «ТБАНК»"),
    "alfa":  ("044525593", "АО «АЛЬФА-БАНК»"),
    "vtb":   ("044525411", "БАНК ВТБ (ПАО)"),
    "psb":   ("044525555", "ПАО «ПРОМСВЯЗЬБАНК»"),
}


def acc(seed_str):
    """Детерминированный 20-значный расчётный счёт."""
    rnd = random.Random(seed_str)
    return "40702810" + "".join(str(rnd.randint(0, 9)) for _ in range(12))


def acc_ip(seed_str):
    rnd = random.Random(seed_str)
    return "40802810" + "".join(str(rnd.randint(0, 9)) for _ in range(12))


def q(dt_start_month):
    """Возвращает (начало, конец) квартала по первому месяцу."""
    y = 2025
    start = date(y, dt_start_month, 1)
    # конец квартала = последний день 3-го месяца
    m = dt_start_month + 2
    if m == 12:
        end = date(y, 12, 31)
    else:
        end = date(y, m + 1, 1) - timedelta(days=1)
    return start, end


def rub(x):
    return f"{x:.2f}"


# ---------------------------------------------------------------------------
# Модель транзакции
# ---------------------------------------------------------------------------

class Tx:
    __slots__ = ("dt", "amount", "direction", "counterparty", "inn", "kpp",
                 "cp_acc", "cp_bik", "cp_bank", "purpose", "category")

    def __init__(self, dt, amount, direction, counterparty, inn, purpose,
                 category="", kpp="", cp_acc="", cp_bik="", cp_bank=""):
        self.dt = dt
        self.amount = amount           # положительное число
        self.direction = direction     # "in" / "out"
        self.counterparty = counterparty
        self.inn = inn
        self.kpp = kpp
        self.cp_acc = cp_acc
        self.cp_bik = cp_bik
        self.cp_bank = cp_bank
        self.purpose = purpose
        self.category = category


def sort_txs(txs):
    return sorted(txs, key=lambda t: (t.dt, 0 if t.direction == "in" else 1))


def running_balance(txs, opening):
    """Возвращает список (дата, баланс_на_конец_дня) и минимальный баланс."""
    bal = opening
    per_day = {}
    for t in sort_txs(txs):
        bal += t.amount if t.direction == "in" else -t.amount
        per_day[t.dt] = bal
    days = sorted(per_day)
    min_bal = min(per_day.values()) if per_day else opening
    return per_day, min_bal


# ---------------------------------------------------------------------------
# Генераторы профилей бизнеса
# ---------------------------------------------------------------------------

def workdays(start, end):
    d = start
    while d <= end:
        if d.weekday() < 5:
            yield d
        d += timedelta(days=1)


def each_month(start, end):
    """Итерирует по (год, месяц) внутри периода."""
    y, m = start.year, start.month
    while (y, m) <= (end.year, end.month):
        yield y, m
        m += 1
        if m > 12:
            m = 1
            y += 1


def day_in_month(y, m, day):
    # безопасно выбираем день, не выходя за пределы месяца
    from calendar import monthrange
    day = min(day, monthrange(y, m)[1])
    return date(y, m, day)


# ---- Профиль 1: Розница-ИП, УСН 6% (магазин одежды) ----

def profile_retail_ip(start, end, gap=False):
    txs = []
    # Эквайринг — почти ежедневно (пн-пт + сб), приход от банка-эквайера
    acq_bik, acq_bank = BANKS["sber"]
    for d in workdays(start, end):
        base = random.randint(18000, 65000)
        # выручка выше по пятницам
        if d.weekday() == 4:
            base = int(base * 1.4)
        txs.append(Tx(d, base, "in", "ПАО СБЕРБАНК (эквайринг)", "7707083893",
                      "Возмещение по эквайрингу по договору №ЭКВ-114/2024. НДС не облагается",
                      "Выручка", cp_bik=acq_bik, cp_bank=acq_bank))
    # Аренда — 1-3 числа
    for y, m in each_month(start, end):
        d = day_in_month(y, m, 3)
        txs.append(Tx(d, 95000, "out", "ООО «ТЦ Меридиан»", "7719854120",
                      f"Оплата аренды торгового помещения за {m:02d}.{y}. НДС не облагается",
                      "Аренда", kpp="771901001"))
    # Зарплата — 2 продавца, 10 и 25 числа
    for y, m in each_month(start, end):
        for day, part in ((10, "аванс"), (25, "окончательный расчёт")):
            dd = day_in_month(y, m, day)
            for name in ("Смирнова А. В.", "Кузьмина О. Н."):
                amt = 21000 if part == "аванс" else 24000
                txs.append(Tx(dd, amt, "out", f"ИП Петров И.С. // {name}", "",
                              f"Заработная плата ({part}) за {m:02d}.{y}. НДФЛ удержан",
                              "Зарплата"))
    # НДФЛ и взносы — 28 числа
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 28)
        txs.append(Tx(dd, 13000, "out", TAX_NAME, TAX_INN,
                      f"НДФЛ с заработной платы за {m:02d}.{y}", "Налоги",
                      kpp=TAX_KPP, cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))
        txs.append(Tx(dd, 27000, "out", SFR_NAME, SFR_INN,
                      f"Страховые взносы за {m:02d}.{y}", "Взносы", kpp=TAX_KPP))
    # Поставщики одежды — крупные закупки 1-2 раза в месяц
    suppliers = [
        ("ООО «Модный Дом Опт»", "7801456712", "771001001"),
        ("ООО «Текстиль-Трейд»", "5024112233", "502401001"),
    ]
    for y, m in each_month(start, end):
        for i, (nm, inn, kpp) in enumerate(suppliers):
            dd = day_in_month(y, m, 12 + i * 6)
            amt = random.randint(140000, 260000)
            txs.append(Tx(dd, amt, "out", nm, inn,
                          f"Оплата по договору поставки товара №{100+i}/2025, счёт от {dd.strftime('%d.%m.%Y')}. В т.ч. НДС 20%",
                          "Поставщики", kpp=kpp))
    # Авансовый платёж УСН 6% — в конце квартала (25 число месяца после квартала — но у нас квартал целиком, поставим в конце)
    dd = day_in_month(end.year, end.month, 25)
    txs.append(Tx(dd, 62000, "out", TAX_NAME, TAX_INN,
                  "Авансовый платёж по УСН (доходы, 6%) за отчётный квартал",
                  "Налоги", kpp=TAX_KPP, cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))
    # Эквайринговая комиссия — раз в месяц
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 5)
        txs.append(Tx(dd, random.randint(9000, 14000), "out", "ПАО СБЕРБАНК", "7707083893",
                      "Комиссия за услуги эквайринга. НДС не облагается", "Комиссии"))

    if gap:
        _inject_gap(txs, start, end, kind="retail")
    return txs


# ---- Профиль 2: Услуги-ООО, УСН 15% (IT/маркетинг агентство) ----

def profile_services_ooo(start, end, gap=False):
    txs = []
    clients = [
        ("ООО «Гранд Девелопмент»", "7736251489", "773601001"),
        ("АО «ФармаЛогистика»", "7728198765", "772801001"),
        ("ООО «Северный Ветер»", "7810334455", "781001001"),
    ]
    # Приход от клиентов — 2-4 раза в месяц по актам
    for y, m in each_month(start, end):
        for i, (nm, inn, kpp) in enumerate(clients):
            dd = day_in_month(y, m, 8 + i * 7)
            amt = random.randint(180000, 420000)
            txs.append(Tx(dd, amt, "in", nm, inn,
                          f"Оплата по договору оказания услуг №{200+i}/2025, акт от {dd.strftime('%d.%m.%Y')}. НДС не облагается (УСН)",
                          "Выручка", kpp=kpp))
    # Аренда офиса
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 5)
        txs.append(Tx(dd, 130000, "out", "ООО «Бизнес-Парк Румянцево»", "7729112233",
                      f"Арендная плата за офис за {m:02d}.{y}. В т.ч. НДС 20%",
                      "Аренда", kpp="772901001"))
    # Зарплата — команда 4 чел, 10 и 25
    team = ["Иванов П. Р.", "Соколова Е. Д.", "Морозов А. К.", "Волкова Н. С."]
    for y, m in each_month(start, end):
        for day, part in ((10, "аванс"), (25, "зарплата")):
            dd = day_in_month(y, m, day)
            for name in team:
                amt = random.randint(28000, 45000)
                txs.append(Tx(dd, amt, "out", f"ООО «Диджитал Сервис» // {name}", "",
                              f"Выплата: {part} за {m:02d}.{y}", "Зарплата"))
    # НДФЛ / взносы
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 28)
        txs.append(Tx(dd, 42000, "out", TAX_NAME, TAX_INN,
                      f"НДФЛ за {m:02d}.{y}", "Налоги", kpp=TAX_KPP,
                      cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))
        txs.append(Tx(dd, 88000, "out", SFR_NAME, SFR_INN,
                      f"Страховые взносы за {m:02d}.{y}", "Взносы", kpp=TAX_KPP))
    # Подрядчики (дизайн, реклама)
    subs = [
        ("ООО «Яндекс»", "7736207543", "770601001", "Рекламные услуги (Яндекс.Директ)"),
        ("ИП Гусев Д.А. (фриланс-дизайн)", "500112223344", "", "Услуги дизайна по договору подряда"),
    ]
    for y, m in each_month(start, end):
        for i, (nm, inn, kpp, desc) in enumerate(subs):
            dd = day_in_month(y, m, 15 + i * 4)
            amt = random.randint(40000, 130000)
            txs.append(Tx(dd, amt, "out", nm, inn,
                          f"{desc} за {m:02d}.{y}", "Подрядчики", kpp=kpp))
    # Авансовый УСН 15%
    dd = day_in_month(end.year, end.month, 25)
    txs.append(Tx(dd, 95000, "out", TAX_NAME, TAX_INN,
                  "Авансовый платёж по УСН (доходы минус расходы, 15%) за квартал",
                  "Налоги", kpp=TAX_KPP, cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))

    if gap:
        _inject_gap(txs, start, end, kind="services")
    return txs


# ---- Профиль 3: E-commerce / маркетплейсы ----

def profile_ecommerce(start, end, gap=False):
    txs = []
    # Выплаты от маркетплейсов — еженедельно (после удержания комиссии)
    mps = [
        ("ООО «Вайлдберриз»", "7721546864", "500301001", "Wildberries"),
        ("ООО «Интернет Решения» (OZON)", "7704217370", "770401001", "Ozon"),
        ("ООО «Яндекс.Маркет»", "9704254424", "770401001", "Яндекс Маркет"),
    ]
    d = start
    week = 0
    while d <= end:
        if d.weekday() == 0:  # понедельник — день выплат
            for nm, inn, kpp, short in mps:
                amt = random.randint(120000, 480000)
                txs.append(Tx(d, amt, "in", nm, inn,
                              f"Выплата по агентскому договору ({short}), реестр за неделю. Комиссия удержана. НДС не облагается",
                              "Выручка-МП", kpp=kpp))
        week += 1
        d += timedelta(days=1)
    # Поставщики товара — крупная предоплата 2 раза в месяц
    suppliers = [
        ("ООО «Гуанчжоу Импорт»", "7743998877", "774301001"),
        ("ООО «СкладОпт»", "5047223344", "504701001"),
    ]
    for y, m in each_month(start, end):
        for i, (nm, inn, kpp) in enumerate(suppliers):
            dd = day_in_month(y, m, 6 + i * 10)
            amt = random.randint(350000, 700000)
            txs.append(Tx(dd, amt, "out", nm, inn,
                          f"Предоплата за товар по договору №{300+i}/2025. В т.ч. НДС 20%",
                          "Поставщики", kpp=kpp))
    # Логистика / фулфилмент
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 14)
        txs.append(Tx(dd, random.randint(45000, 90000), "out", "ООО «СДЭК-Логистика»",
                      "5406447262", f"Услуги доставки и фулфилмента за {m:02d}.{y}. В т.ч. НДС 20%",
                      "Логистика", kpp="540601001"))
    # Реклама на МП
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 18)
        txs.append(Tx(dd, random.randint(60000, 150000), "out", "ООО «Вайлдберриз»",
                      "7721546864", f"Оплата продвижения (реклама) за {m:02d}.{y}",
                      "Реклама", kpp="500301001"))
    # Зарплата (менеджер + упаковщики)
    team = ["Лебедев М. О.", "Зайцева И. П.", "Орлов С. В."]
    for y, m in each_month(start, end):
        for day, part in ((10, "аванс"), (25, "зарплата")):
            dd = day_in_month(y, m, day)
            for name in team:
                txs.append(Tx(dd, random.randint(23000, 38000), "out",
                              f"ООО «Маркет Стар» // {name}", "",
                              f"{part} за {m:02d}.{y}", "Зарплата"))
    # Налоги/взносы
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 28)
        txs.append(Tx(dd, 35000, "out", TAX_NAME, TAX_INN,
                      f"НДФЛ за {m:02d}.{y}", "Налоги", kpp=TAX_KPP,
                      cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))
        txs.append(Tx(dd, 72000, "out", SFR_NAME, SFR_INN,
                      f"Страховые взносы за {m:02d}.{y}", "Взносы", kpp=TAX_KPP))
    # УСН
    dd = day_in_month(end.year, end.month, 25)
    txs.append(Tx(dd, 140000, "out", TAX_NAME, TAX_INN,
                  "Авансовый платёж по УСН за квартал", "Налоги",
                  kpp=TAX_KPP, cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))

    if gap:
        _inject_gap(txs, start, end, kind="ecom")
    return txs


# ---- Профиль 4: Кафе / общепит ----

def profile_cafe(start, end, gap=False):
    txs = []
    acq_bik, acq_bank = BANKS["tbank"]
    # Выручка эквайринг — ежедневно (кафе работает 7/7)
    d = start
    while d <= end:
        base = random.randint(22000, 55000)
        if d.weekday() >= 4:  # пт-вс выше
            base = int(base * 1.5)
        txs.append(Tx(d, base, "in", "АО «ТБАНК» (эквайринг)", "7710140679",
                      "Зачисление по операциям эквайринга. НДС не облагается",
                      "Выручка", cp_bik=acq_bik, cp_bank=acq_bank))
        d += timedelta(days=1)
    # Аренда помещения (высокая — центр)
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 2)
        txs.append(Tx(dd, 180000, "out", "ИП Тарасов В. Г.", "770912345678",
                      f"Аренда помещения общепита за {m:02d}.{y}. НДС не облагается",
                      "Аренда"))
    # Поставщики продуктов — часто, 3 раза в неделю
    food_suppliers = [
        ("ООО «Метро Кэш энд Керри»", "7704218694", "997750001"),
        ("ООО «ПродОпт Свежесть»", "5029223311", "502901001"),
        ("ООО «Мясной Двор»", "5044119988", "504401001"),
    ]
    for d in workdays(start, end):
        if d.weekday() in (0, 2, 4):
            nm, inn, kpp = random.choice(food_suppliers)
            amt = random.randint(28000, 85000)
            txs.append(Tx(d, amt, "out", nm, inn,
                          f"Оплата за продукты питания, накладная от {d.strftime('%d.%m.%Y')}. В т.ч. НДС",
                          "Продукты", kpp=kpp))
    # Зарплата — повара, официанты (6 чел)
    team = ["Николаев Р. А.", "Егорова Т. С.", "Павлов Д. И.",
            "Сидорова М. В.", "Козлов А. А.", "Белова Ю. Н."]
    for y, m in each_month(start, end):
        for day, part in ((10, "аванс"), (25, "зарплата")):
            dd = day_in_month(y, m, day)
            for name in team:
                txs.append(Tx(dd, random.randint(20000, 35000), "out",
                              f"ООО «Кафе Уют» // {name}", "",
                              f"{part} за {m:02d}.{y}", "Зарплата"))
    # Коммуналка
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 15)
        txs.append(Tx(dd, random.randint(35000, 55000), "out", "АО «Мосэнергосбыт»",
                      "7736520080", f"Оплата электроэнергии за {m:02d}.{y}. В т.ч. НДС 20%",
                      "Коммуналка", kpp="770101001"))
    # Налоги/взносы
    for y, m in each_month(start, end):
        dd = day_in_month(y, m, 28)
        txs.append(Tx(dd, 30000, "out", TAX_NAME, TAX_INN,
                      f"НДФЛ за {m:02d}.{y}", "Налоги", kpp=TAX_KPP,
                      cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))
        txs.append(Tx(dd, 95000, "out", SFR_NAME, SFR_INN,
                      f"Страховые взносы за {m:02d}.{y}", "Взносы", kpp=TAX_KPP))
    # УСН
    dd = day_in_month(end.year, end.month, 25)
    txs.append(Tx(dd, 78000, "out", TAX_NAME, TAX_INN,
                  "Авансовый платёж по УСН за квартал", "Налоги",
                  kpp=TAX_KPP, cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))

    if gap:
        _inject_gap(txs, start, end, kind="cafe")
    return txs


# ---------------------------------------------------------------------------
# Инъекция кассового разрыва
# ---------------------------------------------------------------------------

def _inject_gap(txs, start, end, kind):
    """
    Делает так, чтобы в один день накопленный дневной баланс ушёл в минус:
    крупная предоплата поставщику + налоговый платёж совпадают ДО прихода
    денег от клиентов. Разрыв закладываем во втором месяце квартала (день 20).
    """
    months = list(each_month(start, end))
    y, m = months[1]  # второй месяц
    gap_day = day_in_month(y, m, 20)

    if kind == "retail":
        supplier = ("ООО «Модный Дом Опт»", "7801456712", "771001001", 480000)
    elif kind == "services":
        supplier = ("ООО «Гранд Продакшн»", "7736998811", "773601001", 520000)
    elif kind == "ecom":
        supplier = ("ООО «Гуанчжоу Импорт»", "7743998877", "774301001", 950000)
    else:  # cafe
        supplier = ("ООО «Метро Кэш энд Керри»", "7704218694", "997750001", 340000)

    nm, inn, kpp, amt = supplier
    # Крупная внеплановая предоплата
    txs.append(Tx(gap_day, amt, "out", nm, inn,
                  f"Крупная предоплата по договору поставки №999/2025 (срочный заказ). В т.ч. НДС 20%",
                  "Поставщики-разрыв", kpp=kpp))
    # Совпавший налоговый платёж в тот же день
    txs.append(Tx(gap_day, int(amt * 0.35), "out", TAX_NAME, TAX_INN,
                  "Уплата НДС/налога по требованию. Срок — сегодня",
                  "Налоги-разрыв", kpp=TAX_KPP, cp_acc=TAX_ACC, cp_bik=TAX_BIK, cp_bank=TAX_BANK))
    # Приход от клиента приходит ПОЗЖЕ (через 3 дня) — деньги «в пути»
    late = gap_day + timedelta(days=3)
    if late > end:
        late = end
    txs.append(Tx(late, int(amt * 1.1), "in", "ООО «Крупный Клиент»", "7701555666",
                  "Оплата по договору (задержана, поступила позже срока). НДС не облагается",
                  "Выручка", kpp="770101001"))


# ---------------------------------------------------------------------------
# Записчики форматов
# ---------------------------------------------------------------------------

def write_csv_bank(path, txs, own_acc, delimiter=";"):
    """Именованный формат банковской выписки (parseBankStatementRows)."""
    header = ["Дата операции", "Номер пп", "Тип операции",
              "Наименование контрагента", "ИНН контрагента", "КПП контрагента",
              "Расчетный счет контрагента", "БИК банка контрагента",
              "Банк контрагента", "Назначение платежа",
              "Сумма дебет (руб.)", "Сумма кредит (руб.)", "Валюта",
              "Остаток на конец дня (руб.)", "Категория"]
    buf = io.StringIO()
    w = csv.writer(buf, delimiter=delimiter, quoting=csv.QUOTE_MINIMAL,
                   lineterminator="\r\n")
    w.writerow(header)
    bal = OPENING_DEFAULT
    n = 0
    for t in sort_txs(txs):
        n += 1
        if t.direction == "in":
            debit, credit = "", rub(t.amount)
            bal += t.amount
        else:
            debit, credit = rub(t.amount), ""
            bal -= t.amount
        w.writerow([
            t.dt.strftime("%d.%m.%Y"), str(n),
            "Приход" if t.direction == "in" else "Расход",
            t.counterparty, t.inn, t.kpp,
            t.cp_acc, t.cp_bik, t.cp_bank, t.purpose,
            debit, credit, "RUB", rub(bal), t.category,
        ])
    with open(path, "w", encoding="utf-8-sig", newline="") as f:
        f.write(buf.getvalue())


def write_csv_legacy(path, txs, delimiter=";"):
    """Позиционный формат: дата;сумма(знак);направление;контрагент;ИНН;назначение."""
    buf = io.StringIO()
    w = csv.writer(buf, delimiter=delimiter, quoting=csv.QUOTE_MINIMAL,
                   lineterminator="\r\n")
    # Заголовок (парсер пропустит его, т.к. дата не распарсится)
    w.writerow(["Дата", "Сумма", "Направление", "Контрагент", "ИНН", "Назначение платежа"])
    for t in sort_txs(txs):
        signed = t.amount if t.direction == "in" else -t.amount
        w.writerow([
            t.dt.strftime("%d.%m.%Y"),
            rub(signed),
            "Приход" if t.direction == "in" else "Расход",
            t.counterparty, t.inn, t.purpose,
        ])
    with open(path, "w", encoding="utf-8-sig", newline="") as f:
        f.write(buf.getvalue())


def write_clientbank(path, txs, own_acc, own_name, own_inn, bank_key,
                     start, end):
    """
    Формат 1CClientBankExchange, Windows-1251, строго по clientbank.go.
    Направление определяется парсером по совпадению ПлательщикСчет/ПолучательСчет
    с РасчСчет из шапки. Поэтому:
      - расход (out): ПлательщикСчет = own_acc, Получатель = контрагент
      - приход (in):  ПолучательСчет = own_acc, Плательщик = контрагент
    """
    bik, bankname = BANKS[bank_key]
    lines = []
    lines.append("1CClientBankExchange")
    lines.append("ВерсияФормата=1.03")
    lines.append("Кодировка=Windows")
    lines.append(f"Отправитель={bankname}")
    lines.append("Получатель=")
    lines.append(f"ДатаСоздания={end.strftime('%d.%m.%Y')}")
    lines.append(f"ВремяСоздания=18:00:00")
    lines.append(f"ДатаНачала={start.strftime('%d.%m.%Y')}")
    lines.append(f"ДатаКонца={end.strftime('%d.%m.%Y')}")
    lines.append(f"РасчСчет={own_acc}")
    lines.append("СекцияРасчСчет")
    lines.append(f"ДатаНачала={start.strftime('%d.%m.%Y')}")
    lines.append(f"ДатаКонца={end.strftime('%d.%m.%Y')}")
    lines.append(f"РасчСчет={own_acc}")
    lines.append(f"НачальныйОстаток={rub(OPENING_DEFAULT)}")
    lines.append("КонецРасчСчет")

    n = 0
    for t in sort_txs(txs):
        n += 1
        # счёт контрагента: используем cp_acc если задан, иначе детерминированно
        cp_acc = t.cp_acc or acc(f"{t.counterparty}{t.inn}")
        lines.append(f"СекцияДокумент=Платежное поручение")
        lines.append(f"Номер={n}")
        lines.append(f"Дата={t.dt.strftime('%d.%m.%Y')}")
        lines.append(f"Сумма={rub(t.amount)}")
        if t.direction == "out":
            # мы платим
            lines.append(f"ПлательщикСчет={own_acc}")
            lines.append(f"Плательщик={own_name}")
            lines.append(f"ПлательщикИНН={own_inn}")
            lines.append(f"ПолучательСчет={cp_acc}")
            lines.append(f"Получатель={t.counterparty}")
            lines.append(f"ПолучательИНН={t.inn}")
        else:
            # нам платят
            lines.append(f"ПлательщикСчет={cp_acc}")
            lines.append(f"Плательщик={t.counterparty}")
            lines.append(f"ПлательщикИНН={t.inn}")
            lines.append(f"ПолучательСчет={own_acc}")
            lines.append(f"Получатель={own_name}")
            lines.append(f"ПолучательИНН={own_inn}")
        lines.append(f"НазначениеПлатежа={t.purpose}")
        lines.append("КонецДокумента")
    lines.append("КонецФайла")

    content = "\r\n".join(lines) + "\r\n"
    with open(path, "wb") as f:
        f.write(content.encode("windows-1251"))


def write_xlsx(path, txs, own_acc):
    from openpyxl import Workbook
    wb = Workbook()
    ws = wb.active
    ws.title = "Выписка"
    header = ["Дата операции", "Номер пп", "Тип операции",
              "Наименование контрагента", "ИНН контрагента", "КПП контрагента",
              "Расчетный счет контрагента", "БИК банка контрагента",
              "Банк контрагента", "Назначение платежа",
              "Сумма дебет (руб.)", "Сумма кредит (руб.)", "Валюта",
              "Остаток на конец дня (руб.)", "Категория"]
    ws.append(header)
    bal = OPENING_DEFAULT
    n = 0
    for t in sort_txs(txs):
        n += 1
        if t.direction == "in":
            debit, credit = None, round(t.amount, 2)
            bal += t.amount
        else:
            debit, credit = round(t.amount, 2), None
            bal -= t.amount
        ws.append([
            t.dt.strftime("%d.%m.%Y"), n,
            "Приход" if t.direction == "in" else "Расход",
            t.counterparty, t.inn, t.kpp, t.cp_acc, t.cp_bik, t.cp_bank,
            t.purpose, debit, credit, "RUB", round(bal, 2), t.category,
        ])
    wb.save(path)


# ---------------------------------------------------------------------------
# Конфигурация счетов бизнесов
# ---------------------------------------------------------------------------

OPENING_DEFAULT = 250000.0

BUSINESSES = {
    "retail_ip": dict(
        name="ИП Петров Иван Сергеевич",
        inn="770912345678",
        acc=acc_ip("retail_ip"),
        bank="sber",
        gen=profile_retail_ip,
        label="Розница-ИП (магазин одежды), УСН 6%",
    ),
    "services_ooo": dict(
        name="ООО «Диджитал Сервис»",
        inn="7727406020",
        acc=acc("services_ooo"),
        bank="tbank",
        gen=profile_services_ooo,
        label="Услуги-ООО (IT/маркетинг агентство), УСН 15%",
    ),
    "ecommerce": dict(
        name="ООО «Маркет Стар»",
        inn="7743011223",
        acc=acc("ecommerce"),
        bank="alfa",
        gen=profile_ecommerce,
        label="E-commerce / маркетплейсы (WB/Ozon/Яндекс), УСН",
    ),
    "cafe": dict(
        name="ООО «Кафе Уют»",
        inn="7701998877",
        acc=acc("cafe"),
        bank="vtb",
        gen=profile_cafe,
        label="Кафе / общепит, УСН",
    ),
}

# Кварталы
Q1 = q(1)   # янв-мар 2025
Q2 = q(4)   # апр-июн 2025
Q3 = q(7)   # июл-сен 2025
Q4 = q(10)  # окт-дек 2025


def qname(period):
    s, _ = period
    return {1: "Q1", 4: "Q2", 7: "Q3", 10: "Q4"}[s.month]


# ---------------------------------------------------------------------------
# План файлов: (имя, бизнес, формат, период, gap)
# ---------------------------------------------------------------------------

PLAN = [
    # --- CSV bank (именованные колонки), ';' ---
    ("retail_ip_Q1_bank_semicolon.csv",      "retail_ip",    "csv_bank_semi", Q1, False),
    ("services_ooo_Q1_bank_semicolon.csv",   "services_ooo", "csv_bank_semi", Q1, True),   # разрыв
    ("ecommerce_Q1_bank_semicolon.csv",      "ecommerce",    "csv_bank_semi", Q1, True),   # разрыв
    ("cafe_Q1_bank_semicolon.csv",           "cafe",         "csv_bank_semi", Q1, False),

    # --- CSV legacy позиционный, ';' ---
    ("retail_ip_Q2_legacy_semicolon.csv",    "retail_ip",    "csv_legacy_semi", Q2, True),  # разрыв
    ("cafe_Q2_legacy_semicolon.csv",         "cafe",         "csv_legacy_semi", Q2, False),

    # --- CSV legacy позиционный, ',' (вариант с запятой) ---
    ("services_ooo_Q2_legacy_comma.csv",     "services_ooo", "csv_legacy_comma", Q2, False),
    ("ecommerce_Q2_bank_comma.csv",          "ecommerce",    "csv_bank_comma", Q2, True),   # разрыв

    # --- 1CClientBankExchange (Windows-1251) ---
    ("retail_ip_Q3_1c.txt",                  "retail_ip",    "clientbank", Q3, False),
    ("services_ooo_Q3_1c.txt",               "services_ooo", "clientbank", Q3, True),   # разрыв
    ("ecommerce_Q3_1c.txt",                  "ecommerce",    "clientbank", Q3, False),
    ("cafe_Q3_1c.txt",                       "cafe",         "clientbank", Q3, True),   # разрыв

    # --- XLSX ---
    ("retail_ip_Q4.xlsx",                    "retail_ip",    "xlsx", Q4, True),   # разрыв
    ("services_ooo_Q4.xlsx",                 "services_ooo", "xlsx", Q4, False),
    ("ecommerce_Q4.xlsx",                    "ecommerce",    "xlsx", Q4, False),
    ("cafe_Q4.xlsx",                         "cafe",         "xlsx", Q4, False),

    # --- доп. смешанные ---
    ("retail_ip_Q1_1c.txt",                  "retail_ip",    "clientbank", Q1, False),
    ("cafe_Q1_bank_comma.csv",               "cafe",         "csv_bank_comma", Q1, False),
]


def main():
    report = []
    for fname, biz_key, fmt, period, gap in PLAN:
        biz = BUSINESSES[biz_key]
        start, end = period
        # ВАЖНО: пересеиваем random детерминированно на каждый файл,
        # чтобы порядок файлов не влиял на содержимое (стабильность)
        random.seed(hash((SEED, fname)) & 0xFFFFFFFF)
        txs = biz["gen"](start, end, gap=gap)
        path = os.path.join(OUT, fname)

        if fmt == "csv_bank_semi":
            write_csv_bank(path, txs, biz["acc"], delimiter=";")
        elif fmt == "csv_bank_comma":
            write_csv_bank(path, txs, biz["acc"], delimiter=",")
        elif fmt == "csv_legacy_semi":
            write_csv_legacy(path, txs, delimiter=";")
        elif fmt == "csv_legacy_comma":
            write_csv_legacy(path, txs, delimiter=",")
        elif fmt == "clientbank":
            write_clientbank(path, txs, biz["acc"], biz["name"], biz["inn"],
                             biz["bank"], start, end)
        elif fmt == "xlsx":
            write_xlsx(path, txs, biz["acc"])
        else:
            raise ValueError(fmt)

        per_day, min_bal = running_balance(txs, OPENING_DEFAULT)
        gap_days = [str(d) for d, b in sorted(per_day.items()) if b < 0]
        report.append(dict(
            file=fname, biz=biz["label"], fmt=fmt, period=qname(period),
            start=str(start), end=str(end), n=len(txs),
            gap_flag=gap, min_bal=round(min_bal, 2),
            gap_days=gap_days,
        ))
        print(f"OK {fname:38s} txs={len(txs):3d} min_bal={min_bal:12.2f} "
              f"gap={'YES' if min_bal < 0 else 'no ':3s} "
              f"{'('+','.join(gap_days)+')' if gap_days else ''}")

    return report


if __name__ == "__main__":
    rep = main()
    # Сводка для README (пересобирается verify/README-скриптом при необходимости).
    import json
    with open(os.path.join(OUT, "_report.json"), "w", encoding="utf-8") as f:
        json.dump(rep, f, ensure_ascii=False, indent=2)
