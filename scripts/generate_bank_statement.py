#!/usr/bin/env python3
import random
import argparse
from datetime import datetime, timedelta

BANKS = [
    "Сбербанк", "ВТБ", "Тинькофф", "Альфа-Банк",
    "Газпромбанк", "Россельхозбанк", "Открытие",
]

COUNTERPARTIES = [
    ("ООО ТехноСервис", "Оплата услуг по договору"),
    ("ИП Сидоров А.В.", "Оплата за поставку оборудования"),
    ("АО Ромашка", "Авансовый платеж по договору поставки"),
    ("ПАО СтройГрупп", "Оплата строительных материалов"),
    ("ООО МедиаПро", "Рекламные услуги"),
    ("ИП Кузнецова М.Н.", "Аренда офисного помещения"),
    ("АО ЛогистикРус", "Транспортно-экспедиционные услуги"),
    ("ООО КлинингПрофи", "Услуги по уборке"),
    ("ЗАО АудитКонсалт", "Консультационные услуги"),
    ("ПАО ЭнергоСбыт", "Оплата электроэнергии"),
    ("ООО ГазСеть", "Оплата газоснабжения"),
    ("МУП Водоканал", "Оплата водоснабжения"),
    ("ООО ИТ-Решения", "Техническое обслуживание серверов"),
    ("АО СофтКорп", "Лицензия на ПО"),
    ("ФНС России", "НДС за 2 квартал 2026 г."),
    ("СФР", "Страховые взносы"),
    ("ООО Аванс", "Выдача подотчетных средств"),
    ("АО АрендаПлюс", "Аренда склада"),
    ("ООО ПечатьЭкспресс", "Полиграфические услуги"),
    ("ИП Волков Д.С.", "Курьерская доставка"),
    ("ООО ОфисМакс", "Канцтовары и расходные материалы"),
    ("АО АвиаСервис", "Авиабилеты командировка"),
    ("ООО ГостиничныйДвор", "Проживание сотрудников"),
    ("ПАО ТелеКом", "Услуги связи"),
    ("ООО РеклФаст", "Контекстная реклама"),
    ("АО ЮрКонсалт", "Юридическое сопровождение"),
    ("ООО БухАутсорс", "Бухгалтерское обслуживание"),
    ("ИП Лебедев К.О.", "Разработка дизайна"),
    ("АО ПроектТех", "Проектно-изыскательские работы"),
    ("ООО МеталлТрейд", "Поставка металлопроката"),
]

CP_TO_CAT = {
    "ФНС России": "Налоги и взносы",
    "СФР": "Налоги и взносы",
    "ИП Кузнецова М.Н.": "Аренда",
    "АО АрендаПлюс": "Аренда",
    "ПАО ЭнергоСбыт": "ЖКХ и коммунальные",
    "ООО ГазСеть": "ЖКХ и коммунальные",
    "МУП Водоканал": "ЖКХ и коммунальные",
    "ООО ИТ-Решения": "ИТ и ПО",
    "АО СофтКорп": "ИТ и ПО",
    "ООО МедиаПро": "Маркетинг",
    "ООО РеклФаст": "Маркетинг",
    "АО ЛогистикРус": "Логистика",
    "ИП Волков Д.С.": "Логистика",
    "ИП Сидоров А.В.": "Поставка товаров",
    "АО Ромашка": "Поставка товаров",
    "ООО МеталлТрейд": "Поставка товаров",
    "ООО ОфисМакс": "Поставка товаров",
    "ООО ТехноСервис": "Услуги",
    "ООО КлинингПрофи": "Услуги",
    "ЗАО АудитКонсалт": "Услуги",
    "ООО ПечатьЭкспресс": "Услуги",
    "АО ЮрКонсалт": "Услуги",
    "ООО БухАутсорс": "Услуги",
    "ПАО СтройГрупп": "Строительство",
    "АО ПроектТех": "Строительство",
    "АО АвиаСервис": "Командировочные",
    "ООО ГостиничныйДвор": "Командировочные",
    "ООО Аванс": "Командировочные",
    "ПАО ТелеКом": "Связь",
    "ИП Лебедев К.О.": "Дизайн",
}

HEADER = [
    "Дата операции",
    "Номер платежного поручения",
    "Тип операции",
    "Наименование контрагента",
    "ИНН контрагента",
    "КПП контрагента",
    "Расчетный счет контрагента",
    "БИК банка контрагента",
    "Банк контрагента",
    "Назначение платежа",
    "Сумма дебет (руб.)",
    "Сумма кредит (руб.)",
    "Валюта",
    "Остаток на конец дня (руб.)",
    "Категория",
]

def fmt_rub(val: float) -> str:
    s = f"{val:.2f}"
    ip, dp = s.split(".")
    groups = []
    while len(ip) > 3:
        groups.insert(0, ip[-3:])
        ip = ip[:-3]
    groups.insert(0, ip)
    return " ".join(groups) + "." + dp

def rand_d(n: int) -> str:
    return "".join(
        [random.choice("123456789")] +
        [random.choice("0123456789") for _ in range(n - 1)]
    )

def rand_inn() -> str:
    return rand_d(4) + " " + "0" + rand_d(5)

def rand_kpp() -> str:
    return rand_d(6) + " " + rand_d(3)

def rand_account() -> str:
    s = rand_d(12)
    return "40702 810 " + s[0] + " " + s[1:5] + " " + s[5:]

def rand_bik() -> str:
    return "04" + rand_d(5) + random.choice("0123456789") + random.choice("0123456789")

def generate(n_rows: int = 80, seed: int = 42, initial_balance: float = 18_500_000.0) -> list:
    random.seed(seed)
    start = datetime(2026, 4, 1)
    end = datetime(2026, 6, 28)
    balance = initial_balance
    rows = []

    for _ in range(n_rows):
        date = start + timedelta(days=random.randint(0, (end - start).days))
        is_credit = random.random() < 0.30
        cp_name, purpose_base = random.choice(COUNTERPARTIES)

        amount = round(random.uniform(100_000, 5_000_000), 2) if is_credit else round(random.uniform(5_000, 2_000_000), 2)
        nds = round(amount * 20 / 120, 2)
        doc_num = str(random.randint(1, 9999))
        purpose = (
            f"{purpose_base} N{doc_num} "
            f"ot {date.strftime('%d.%m.%Y')} g. "
            f"V t.ch. NDS 20% {fmt_rub(nds)} rub."
        )

        if is_credit:
            dv, cv = "", fmt_rub(amount)
            balance = round(balance + amount, 2)
            op_type = "Поступление"
        else:
            dv, cv = fmt_rub(amount), ""
            balance = round(balance - amount, 2)
            op_type = "Списание"

        rows.append([
            date.strftime("%d.%m.%Y"),
            doc_num,
            op_type,
            cp_name,
            rand_inn(),
            rand_kpp(),
            rand_account(),
            rand_bik(),
            random.choice(BANKS),
            purpose,
            dv,
            cv,
            "RUB",
            fmt_rub(balance),
            CP_TO_CAT.get(cp_name, "Прочее"),
        ])

    rows.sort(key=lambda r: datetime.strptime(r[0], "%d.%m.%Y"))
    return rows

def write_csv(rows: list, path: str) -> None:
    lines = [";".join(f'"{h}"' for h in HEADER)]
    for row in rows:
        lines.append(";".join(f'"{str(cell)}"' for cell in row))
    data = "\n".join(lines) + "\n"
    with open(path, "wb") as f:
        f.write(b"\xef\xbb\xbf")
        f.write(data.encode("utf-8"))

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--rows", type=int, default=80)
    parser.add_argument("--out", type=str, default="bank_statement_q2_2026.csv")
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--balance", type=float, default=18_500_000.0)
    args = parser.parse_args()

    rows = generate(n_rows=args.rows, seed=args.seed, initial_balance=args.balance)
    write_csv(rows, args.out)
    print(f"[OK] {len(rows)} rows -> {args.out}")

if __name__ == "__main__":
    main()